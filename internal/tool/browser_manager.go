package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// BrowserManager manages browser instances and tabs
type BrowserManager struct {
	mu          sync.RWMutex
	tabs        map[string]*BrowserTab
	allocCtx    context.Context
	allocCancel context.CancelFunc
}

// BrowserTab represents a browser tab
type BrowserTab struct {
	ID      string
	Ctx     context.Context
	Cancel  context.CancelFunc
	URL     string
	Title   string
	History []string

	// dialogMu guards the native-JS-dialog state below. Dialogs are handled at
	// the CDP layer (Page.javascriptDialogOpening), so interception survives
	// page navigations and works for dialogs opened before any JS injection.
	dialogMu       sync.Mutex
	dialogPresets  map[string]string        // dialog type -> preset response
	pendingDialogs []map[string]interface{} // dialogs recorded so far
}

// historyCap bounds tab.History so it cannot grow unbounded.
const historyCap = 200

// jsQuote returns s as a JSON string literal, safe to embed inside a JS
// context. Prevents selector/script injection when interpolating
// user-supplied values into page JavaScript.
func jsQuote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(buf.String(), "\n")
}

var (
	browserManager     *BrowserManager
	browserManagerOnce sync.Once
)

// GetBrowserManager returns the singleton browser manager
func GetBrowserManager() *BrowserManager {
	browserManagerOnce.Do(func() {
		browserManager = &BrowserManager{
			tabs: make(map[string]*BrowserTab),
		}
	})
	return browserManager
}

// Initialize initializes the browser allocator
func (bm *BrowserManager) Initialize() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.allocCtx != nil {
		return nil
	}

	browserPath := bm.findBrowser()
	if browserPath == "" {
		return fmt.Errorf("browser not found: please install Google Chrome or Microsoft Edge, or set CHROME_PATH or EDGE_PATH environment variable")
	}

	// Check if running in a sandboxed environment (e.g., Tauri, Flatpak, Snap)
	isSandboxed := isSandboxedEnvironment()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// Use headless mode in sandboxed environments or when explicitly requested
		chromedp.Flag("headless", isSandboxed || os.Getenv("BROWSER_HEADLESS") == "true"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		// Do not hard-code a debugging port: chromedp picks a free random port
		// (--remote-debugging-port=0) so leftover Chrome instances from crashed
		// runs can never block browser tools from starting again.
		chromedp.Flag("disable-logging", true),
		chromedp.Flag("log-level", "3"),
		chromedp.Flag("enable-logging", false),
		chromedp.ExecPath(browserPath),
	)

	if !isSandboxed {
		opts = append(opts, chromedp.Flag("start-maximized", true))
	}

	bm.allocCtx, bm.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	return nil
}

// isSandboxedEnvironment detects if running in a sandboxed environment
func isSandboxedEnvironment() bool {
	// Check for Tauri environment
	if os.Getenv("TAURI_ENV") != "" || os.Getenv("TAURI_APP_DIR") != "" {
		return true
	}

	// Check for Flatpak sandbox
	if os.Getenv("FLATPAK_ID") != "" {
		return true
	}

	// Check for Snap sandbox
	if os.Getenv("SNAP") != "" {
		return true
	}

	// Check if running as a bundled app (common in Tauri)
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		return true
	}

	// Check for AppImage or other containerized environments
	if os.Getenv("APPIMAGE") != "" {
		return true
	}

	return false
}

func (bm *BrowserManager) findBrowser() string {
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	if envPath := os.Getenv("EDGE_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath
		}
	}

	var paths []string

	if runtime.GOOS == "windows" {
		paths = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			paths = append(paths,
				filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			paths = append(paths,
				filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
		if pf86 := os.Getenv("ProgramFiles(x86)"); pf86 != "" {
			paths = append(paths,
				filepath.Join(pf86, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(pf86, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
	} else if runtime.GOOS == "darwin" {
		paths = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	} else {
		paths = []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
			"microsoft-edge-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/local/bin/google-chrome",
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
		}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	for _, p := range []string{"chrome", "google-chrome", "chromium", "microsoft-edge"} {
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}

	return ""
}

// Close closes the browser manager and all tabs. Safe to call multiple times;
// the next NewTab call will start a brand-new Chrome instance.
func (bm *BrowserManager) Close() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, tab := range bm.tabs {
		if tab.Cancel != nil {
			tab.Cancel()
		}
	}
	bm.tabs = make(map[string]*BrowserTab)

	if bm.allocCancel != nil {
		bm.allocCancel()
		bm.allocCtx = nil
		bm.allocCancel = nil
	}
}

// Reset is an alias for Close: tears down every tab and the Chrome instance so
// the next use starts fresh (used to recover from a crashed browser).
func (bm *BrowserManager) Reset() { bm.Close() }

// TabCount returns the number of live tabs.
func (bm *BrowserManager) TabCount() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return len(bm.tabs)
}

// NewTab creates a new browser tab or returns the existing one if already
// present. The tab's context installs a CDP-level JS dialog handler so that
// alert/confirm/prompt never block automation — including dialogs raised
// during page load, before any script could be injected.
func (bm *BrowserManager) NewTab(tabID string) (*BrowserTab, error) {
	bm.mu.RLock()
	if existingTab, ok := bm.tabs[tabID]; ok {
		bm.mu.RUnlock()
		return existingTab, nil
	}
	needsInit := bm.allocCtx == nil
	bm.mu.RUnlock()

	if needsInit {
		if err := bm.Initialize(); err != nil {
			return nil, err
		}
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Re-check under the write lock: a concurrent NewTab with the same ID may
	// have created the tab while we were initializing.
	if existingTab, ok := bm.tabs[tabID]; ok {
		return existingTab, nil
	}
	if bm.allocCtx == nil {
		return nil, fmt.Errorf("browser not initialized")
	}

	tabCtx, tabCancel := chromedp.NewContext(bm.allocCtx)

	tab := &BrowserTab{
		ID:            tabID,
		Ctx:           tabCtx,
		Cancel:        tabCancel,
		History:       make([]string, 0),
		dialogPresets: make(map[string]string),
	}

	chromedp.ListenTarget(tabCtx, func(ev interface{}) {
		if dlg, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			bm.handleDialogOpening(tab, dlg)
		}
	})

	bm.tabs[tabID] = tab
	return tab, nil
}

// GetTab gets a tab by ID
func (bm *BrowserManager) GetTab(tabID string) (*BrowserTab, bool) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	tab, ok := bm.tabs[tabID]
	return tab, ok
}

// CloseTab closes a specific tab
func (bm *BrowserManager) CloseTab(tabID string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if tab, ok := bm.tabs[tabID]; ok {
		if tab.Cancel != nil {
			tab.Cancel()
		}
		delete(bm.tabs, tabID)
	}
}

// ============================================================================
// Navigation & page state
// ============================================================================

// recordURL updates tab.URL/History for a navigation. Callers must not hold
// bm.mu. History records every distinct URL the tab has been on, capped to
// historyCap entries.
func (bm *BrowserManager) recordURL(tabID, url string) {
	if url == "" || url == "about:blank" {
		return
	}
	bm.mu.Lock()
	defer bm.mu.Unlock()
	tab, ok := bm.tabs[tabID]
	if !ok {
		return
	}
	if tab.URL == url {
		return
	}
	tab.History = append(tab.History, url)
	if len(tab.History) > historyCap {
		tab.History = tab.History[len(tab.History)-historyCap:]
	}
	tab.URL = url
}

// pageSnapshot reads the live URL/title/readyState of a tab in one round trip.
func (bm *BrowserManager) pageSnapshot(tabID string) (url, title, readyState string, err error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return "", "", "", fmt.Errorf("tab not found: %s", tabID)
	}
	ctx, cancel := context.WithTimeout(tab.Ctx, 15*time.Second)
	defer cancel()
	err = chromedp.Run(ctx,
		chromedp.Title(&title),
		chromedp.EvaluateAsDevTools("window.location.href", &url),
		chromedp.EvaluateAsDevTools("document.readyState", &readyState),
	)
	return url, title, readyState, err
}

// syncTabState refreshes tab.URL/Title/History from the live page after a
// navigation that was not initiated by NavigateAndGetContent (click, back,
// forward, refresh, ...).
func (bm *BrowserManager) syncTabState(tabID string) {
	url, title, _, err := bm.pageSnapshot(tabID)
	if err != nil {
		return
	}
	bm.recordURL(tabID, url)
	if title == "" {
		return
	}
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if tab, ok := bm.tabs[tabID]; ok {
		tab.Title = title
	}
}

// settlePage gives an in-flight navigation a short window to reach
// readyState=complete, then syncs the tab bookkeeping. Returns quickly when
// nothing navigated (e.g. SPA interactions).
func (bm *BrowserManager) settlePage(tabID string) {
	deadline := time.Now().Add(4 * time.Second)
	for {
		_, _, ready, err := bm.pageSnapshot(tabID)
		if err != nil {
			return
		}
		if ready == "complete" {
			bm.syncTabState(tabID)
			return
		}
		if time.Now().After(deadline) {
			bm.syncTabState(tabID)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// NavigateAndGetContent navigates to URL and gets page content in single call.
// A bounded internal timeout guarantees the call terminates even when the page
// never finishes loading.
func (bm *BrowserManager) NavigateAndGetContent(tabID string, url string) (string, string, error) {
	bm.mu.RLock()
	tab, ok := bm.tabs[tabID]
	bm.mu.RUnlock()

	if !ok {
		return "", "", fmt.Errorf("tab not found: %s", tabID)
	}

	var title string
	var text string

	nctx, cancel := context.WithTimeout(tab.Ctx, 120*time.Second)
	defer cancel()

	err := chromedp.Run(nctx,
		chromedp.Navigate(url),
		chromedp.Title(&title),
		chromedp.Text("body", &text),
	)

	if err != nil {
		return "", "", fmt.Errorf("failed to navigate and get content: %w", err)
	}

	bm.recordURL(tabID, url)
	bm.mu.Lock()
	if t, ok := bm.tabs[tabID]; ok && title != "" {
		t.Title = title
	}
	bm.mu.Unlock()

	return title, text, nil
}

// ============================================================================
// Interaction primitives
// ============================================================================

// Click clicks an element by selector using real CDP mouse input (press +
// release at the element center after scrolling it into view). Synthetic JS
// clicks cannot drive hover-dependent controls, drag sequences or new-tab
// links the same way.
func (bm *BrowserManager) Click(tabID string, selector string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.WaitReady(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Sleep(600*time.Millisecond),
	)
	if err != nil {
		return err
	}

	// A click may have triggered navigation; settle and refresh bookkeeping.
	bm.settlePage(tabID)
	return nil
}

// ClearInput clears an input/textarea using the native value setter and
// dispatches input/change events, so controlled components (React/Vue) observe
// the change. Returns an error when the selector does not resolve to an
// INPUT/TEXTAREA.
func (bm *BrowserManager) ClearInput(tabID string, selector string) error {
	script := `(function() {
		var el = document.querySelector(` + jsQuote(selector) + `);
		if (!el) return 'not_found';
		if (el.tagName !== 'INPUT' && el.tagName !== 'TEXTAREA') return 'not_input';
		var proto = (el.tagName === 'TEXTAREA') ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype;
		if (proto) {
			var d = Object.getOwnPropertyDescriptor(proto, 'value');
			if (d && d.set) {
				d.set.call(el, '');
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				return 'cleared';
			}
		}
		el.value = '';
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
		return 'cleared';
	})()`

	res, err := bm.ExecuteJS(tabID, script)
	if err != nil {
		return err
	}
	switch s := fmt.Sprintf("%v", res); s {
	case "not_found":
		return fmt.Errorf("element not found: %s", selector)
	case "not_input":
		return fmt.Errorf("selector %q is not an input or textarea", selector)
	}
	return nil
}

// Type types text into an element via real keystrokes (focus + key events).
// Real typing works with autocomplete/combobox/datepicker style controls that
// need keydown/keyup, and with React controlled inputs. If the element is not
// interactable (e.g. hidden), it falls back to direct value assignment so the
// old behaviour is preserved.
func (bm *BrowserManager) Type(tabID string, selector string, text string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 90*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.WaitReady(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
	if err == nil {
		return nil
	}

	// Fallback: invisible/edge-case fields get their value assigned directly
	// (chromedp SetValue dispatches input/change events too).
	ctx2, cancel2 := context.WithTimeout(tab.Ctx, 30*time.Second)
	defer cancel2()
	if err2 := chromedp.Run(ctx2, chromedp.SetValue(selector, text)); err2 != nil {
		return fmt.Errorf("failed to type text: %v (send keys: %v)", err2, err)
	}
	return nil
}

// Scroll scrolls the page by the given delta (window.scrollBy).
func (bm *BrowserManager) Scroll(tabID string, x, y int64) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(fmt.Sprintf("window.scrollBy(%d, %d)", x, y), nil),
	)
}

// ScrollToTop scrolls the page back to the top. window.scrollBy(0,0) is a
// no-op, so this uses scrollTo.
func (bm *BrowserManager) ScrollToTop(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.scrollTo(0, 0)", nil),
	)
}

// ScrollToElement scrolls to an element. Fails when the selector does not
// match any element (previously it silently succeeded).
func (bm *BrowserManager) ScrollToElement(tabID string, selector string) error {
	script := `(function() {
		var element = document.querySelector(` + jsQuote(selector) + `);
		if (!element) return false;
		element.scrollIntoView({ behavior: 'smooth', block: 'center' });
		return true;
	})()`

	res, err := bm.ExecuteJS(tabID, script)
	if err != nil {
		return err
	}
	if ok, _ := res.(bool); !ok {
		return fmt.Errorf("element not found: %s", selector)
	}
	return nil
}

// Back goes back in history and waits for the resulting page to settle.
func (bm *BrowserManager) Back(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.history.back()", nil),
		chromedp.Sleep(400*time.Millisecond),
	)
	if err != nil {
		return err
	}
	bm.settlePage(tabID)
	return nil
}

// ExecuteJS executes JavaScript and returns the serialized result.
func (bm *BrowserManager) ExecuteJS(tabID string, script string) (interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var result interface{}
	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(script, &result),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	return result, nil
}

// ExecuteJSAwait executes JavaScript and, when the expression yields a
// promise, awaits its resolution before returning (like an async IIFE in the
// DevTools console). Non-promise expressions behave exactly like ExecuteJS.
func (bm *BrowserManager) ExecuteJSAwait(tabID string, script string) (interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var remote *cruntime.RemoteObject
	var exc *cruntime.ExceptionDetails
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var e error
		remote, exc, e = cruntime.Evaluate(script).
			WithReturnByValue(true).
			WithAwaitPromise(true).
			Do(ctx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}
	if remote == nil {
		return nil, nil
	}
	if exc != nil {
		detail := exc.Exception.Description
		if detail == "" {
			detail = exc.Text
		}
		return nil, fmt.Errorf("script error: %s", detail)
	}
	if len(remote.Value) == 0 || string(remote.Value) == "null" {
		return nil, nil
	}
	var result interface{}
	if err := json.Unmarshal(remote.Value, &result); err != nil {
		return nil, fmt.Errorf("failed to decode script result: %w", err)
	}
	return result, nil
}

// GetPageContent gets the HTML content of the page
func (bm *BrowserManager) GetPageContent(tabID string) (string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return "", fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var html string
	err := chromedp.Run(ctx,
		chromedp.OuterHTML("html", &html),
	)

	if err != nil {
		return "", fmt.Errorf("failed to get page content: %w", err)
	}

	return html, nil
}

// GetPageText gets the text content of the page
func (bm *BrowserManager) GetPageText(tabID string) (string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return "", fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var text string
	err := chromedp.Run(ctx,
		chromedp.Text("body", &text),
	)

	if err != nil {
		return "", fmt.Errorf("failed to get page text: %w", err)
	}

	return text, nil
}

// Screenshot takes a screenshot of the page
func (bm *BrowserManager) Screenshot(tabID string) ([]byte, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var buf []byte
	err := chromedp.Run(ctx,
		chromedp.FullScreenshot(&buf, 90),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to take screenshot: %w", err)
	}

	return buf, nil
}

// BrowserImageInfo describes one <img> element rendered in the live DOM.
type BrowserImageInfo struct {
	Src    string `json:"src"`
	Alt    string `json:"alt"`
	Width  int64  `json:"width"`  // naturalWidth (0 when not yet loaded)
	Height int64  `json:"height"` // naturalHeight
}

// GetImagesWithInfo collects all rendered <img> elements with their natural
// dimensions from the live DOM (images added by JavaScript are included).
func (bm *BrowserManager) GetImagesWithInfo(tabID string) ([]BrowserImageInfo, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var imgs []BrowserImageInfo
	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`Array.from(document.images).map(function(im) {
			return { src: im.currentSrc || im.src, alt: im.alt || '', width: im.naturalWidth, height: im.naturalHeight };
		})`, &imgs),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get images: %w", err)
	}

	return imgs, nil
}

// Forward navigates forward in history and settles afterwards
func (bm *BrowserManager) Forward(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.history.forward()", nil),
		chromedp.Sleep(400*time.Millisecond),
	)
	if err != nil {
		return err
	}
	bm.settlePage(tabID)
	return nil
}

// Refresh refreshes the current page and settles afterwards
func (bm *BrowserManager) Refresh(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.location.reload()", nil),
		chromedp.Sleep(400*time.Millisecond),
	)
	if err != nil {
		return err
	}
	bm.settlePage(tabID)
	return nil
}

// WaitForElement waits for an element to appear on the page
func (bm *BrowserManager) WaitForElement(tabID string, selector string, timeout time.Duration) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, timeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.WaitReady(selector, chromedp.ByQuery),
	)
}

// WaitForLoad waits until document.readyState is "complete" (a plain body
// element exists almost immediately even while resources are still loading).
func (bm *BrowserManager) WaitForLoad(tabID string, timeout time.Duration) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	deadline := time.Now().Add(timeout)
	for {
		var ready string
		ctx, cancel := context.WithTimeout(tab.Ctx, 10*time.Second)
		err := chromedp.Run(ctx,
			chromedp.EvaluateAsDevTools("document.readyState", &ready),
		)
		cancel()

		if err != nil {
			return err
		}
		if ready == "complete" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("page did not finish loading within %s (readyState=%s)", timeout, ready)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// GetPageInfo gets current page information
func (bm *BrowserManager) GetPageInfo(tabID string) (map[string]interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 30*time.Second)
	defer cancel()

	var title string
	var url string
	var readyState string

	err := chromedp.Run(ctx,
		chromedp.Title(&title),
		chromedp.EvaluateAsDevTools("window.location.href", &url),
		chromedp.EvaluateAsDevTools("document.readyState", &readyState),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get page info: %w", err)
	}

	bm.mu.RLock()
	historyLen := len(tab.History)
	bm.mu.RUnlock()

	return map[string]interface{}{
		"title":       title,
		"url":         url,
		"ready_state": readyState,
		"history_len": historyLen,
	}, nil
}

// ClearCache clears localStorage/sessionStorage, the HTTP disk cache and all
// cookies for the browser context.
func (bm *BrowserManager) ClearCache(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 30*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`
			window.localStorage.clear();
			window.sessionStorage.clear();
			true;
		`, nil),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.ClearBrowserCache().Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.ClearBrowserCookies().Do(ctx)
		}),
	)
}

// GetCookies returns every cookie of the browser context — including HttpOnly
// ones that are invisible to document.cookie.
func (bm *BrowserManager) GetCookies(tabID string) ([]map[string]interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 30*time.Second)
	defer cancel()

	var cookies []*network.Cookie
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var e error
		cookies, e = network.GetCookies().Do(ctx)
		return e
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}

	out := make([]map[string]interface{}, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, map[string]interface{}{
			"name":      c.Name,
			"value":     c.Value,
			"domain":    c.Domain,
			"path":      c.Path,
			"secure":    c.Secure,
			"http_only": c.HTTPOnly,
			"session":   c.Session,
		})
	}
	return out, nil
}

// ============================================================================
// Keyboard input
// ============================================================================

// PressKey presses a named key (Enter, Tab, Escape, ArrowUp, F5, ...), a
// chord such as "Control+A", or types arbitrary text into the currently
// focused element. Named keys use native CDP key events (so form submission
// on Enter works), not synthetic JS events.
func (bm *BrowserManager) PressKey(tabID string, key string, times int) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	chordKey, mods := parseKeyChord(key)
	keys := normalizeKeyName(chordKey)
	if keys == "" {
		return fmt.Errorf("invalid key: %s", key)
	}

	if times < 1 {
		times = 1
	}
	if times > 100 {
		times = 100
	}

	actions := make([]chromedp.Action, 0, times)
	for i := 0; i < times; i++ {
		if len(mods) > 0 {
			// KeyModifiers lets a single key event carry the modifier state,
			// which is how apps see Ctrl+A / Ctrl+Enter / etc.
			actions = append(actions, chromedp.KeyEvent(keys, chromedp.KeyModifiers(mods...)))
		} else {
			actions = append(actions, chromedp.KeyEvent(keys))
		}
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, actions...); err != nil {
		return fmt.Errorf("failed to press key %q: %w", key, err)
	}
	return nil
}

// parseKeyChord splits "Control+A" / "ctrl+shift+p" into the actual key plus
// the modifier bitmask. Strings without a leading modifier pass through
// untouched so arbitrary literal text still works.
func parseKeyChord(key string) (string, []input.Modifier) {
	parts := strings.Split(key, "+")
	if len(parts) < 2 {
		return key, nil
	}
	var mods []input.Modifier
	seen := map[input.Modifier]bool{}
	for _, p := range parts[:len(parts)-1] {
		m, ok := modifierFromName(p)
		if !ok {
			return key, nil // not a modifier chord → treat as literal text
		}
		if !seen[m] {
			seen[m] = true
			mods = append(mods, m)
		}
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	// A chord targets the physical key, so "Control+A" must dispatch the
	// lowercase letter — uppercase runes are not in kb.Keys and would be sent
	// as key "Unidentified", breaking Ctrl+A style shortcuts.
	if len(last) == 1 && last[0] >= 'A' && last[0] <= 'Z' {
		last = strings.ToLower(last)
	}
	return last, mods
}

// modifierFromName maps a human modifier name to its CDP modifier bit.
func modifierFromName(name string) (input.Modifier, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "control", "ctrl":
		return input.ModifierCtrl, true
	case "alt", "option":
		return input.ModifierAlt, true
	case "shift":
		return input.ModifierShift, true
	case "meta", "cmd", "command", "win", "windows", "super":
		return input.ModifierMeta, true
	}
	return 0, false
}

// normalizeKeyName converts a human-readable key name into the key string
// accepted by chromedp.KeyEvent / the kb package. Unknown names pass through
// unchanged (so arbitrary text can be typed).
func normalizeKeyName(key string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch lower {
	case "enter", "return":
		return kb.Enter
	case "tab":
		return kb.Tab
	case "escape", "esc":
		return kb.Escape
	case "backspace":
		return kb.Backspace
	case "delete", "del":
		return kb.Delete
	case "arrowup", "up":
		return kb.ArrowUp
	case "arrowdown", "down":
		return kb.ArrowDown
	case "arrowleft", "left":
		return kb.ArrowLeft
	case "arrowright", "right":
		return kb.ArrowRight
	case "home":
		return kb.Home
	case "end":
		return kb.End
	case "pageup", "pgup":
		return kb.PageUp
	case "pagedown", "pgdn":
		return kb.PageDown
	case "space":
		return " "
	case "capslock", "caps lock":
		return kb.CapsLock
	case "control", "ctrl", "ctrlleft":
		return kb.Control
	case "shift", "shiftleft":
		return kb.Shift
	case "alt", "altleft":
		return kb.Alt
	case "meta", "metaleft", "super", "win", "windows":
		return kb.Meta
	}

	// Function keys F1-F12
	if len(lower) >= 2 && lower[0] == 'f' {
		if n, err := strconv.Atoi(lower[1:]); err == nil && n >= 1 && n <= 12 {
			return string(rune(0x0800 + n)) // kb.F1 = \u0801 ... kb.F12 = \u080c
		}
	}

	// Single characters and arbitrary text pass through as-is
	return key
}

// ============================================================================
// Native JS dialog handling (CDP layer)
// ============================================================================

// handleDialogOpening records the dialog and answers it according to the
// preset set via SetDialogResponse, so automation is never blocked by a modal
// dialog. Runs on the tab's event listener; the CDP answer happens on a
// detached goroutine so navigation races cannot deadlock the listener.
func (bm *BrowserManager) handleDialogOpening(tab *BrowserTab, dlg *page.EventJavascriptDialogOpening) {
	typ := string(dlg.Type)
	entry := map[string]interface{}{
		"type":      typ,
		"message":   dlg.Message,
		"url":       dlg.URL,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if dlg.DefaultPrompt != "" {
		entry["default_value"] = dlg.DefaultPrompt
	}

	tab.dialogMu.Lock()
	tab.pendingDialogs = append(tab.pendingDialogs, entry)
	preset, hasPreset := tab.dialogPresets[typ]
	tab.dialogMu.Unlock()

	accept := true
	promptText := ""
	switch dlg.Type {
	case page.DialogTypeConfirm:
		if hasPreset {
			accept = preset == "true"
		} else {
			accept = false // dismissing behaves like clicking "Cancel"
		}
	case page.DialogTypePrompt:
		if hasPreset {
			accept = true
			promptText = preset
		} else if dlg.DefaultPrompt != "" {
			accept = true // accept the pre-filled default, like an idle user
			promptText = dlg.DefaultPrompt
		} else {
			accept = false
		}
	case page.DialogTypeAlert, page.DialogTypeBeforeunload:
		accept = true
	}

	go func() {
		hctx, cancel := context.WithTimeout(tab.Ctx, 10*time.Second)
		defer cancel()
		_ = page.HandleJavaScriptDialog(accept).WithPromptText(promptText).Do(hctx)
	}()
}

// GetPendingDialogs returns all native dialogs captured so far.
func (bm *BrowserManager) GetPendingDialogs(tabID string) ([]map[string]interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}
	tab.dialogMu.Lock()
	defer tab.dialogMu.Unlock()
	out := make([]map[string]interface{}, 0, len(tab.pendingDialogs))
	out = append(out, tab.pendingDialogs...)
	return out, nil
}

// ClearDialogs clears all recorded pending dialogs.
func (bm *BrowserManager) ClearDialogs(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}
	tab.dialogMu.Lock()
	tab.pendingDialogs = nil
	tab.dialogMu.Unlock()
	return nil
}

// SetDialogResponse pre-sets the answer used for future dialogs of the given
// type ("confirm" or "prompt"). For confirm use "true"/"false"; for prompt use
// the text value to return. Presets live on the Go side of the tab, so they
// survive navigations.
func (bm *BrowserManager) SetDialogResponse(tabID string, dialogType string, response string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}
	if dialogType != "confirm" && dialogType != "prompt" {
		return fmt.Errorf("dialog type must be 'confirm' or 'prompt', got %q", dialogType)
	}
	tab.dialogMu.Lock()
	defer tab.dialogMu.Unlock()
	if tab.dialogPresets == nil {
		tab.dialogPresets = make(map[string]string)
	}
	tab.dialogPresets[dialogType] = response
	return nil
}
