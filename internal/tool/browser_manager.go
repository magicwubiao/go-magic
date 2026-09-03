package tool

import (
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
		chromedp.Flag("remote-debugging-port", "9222"),
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

// Close closes the browser manager and all tabs
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

// NewTab creates a new browser tab or returns existing one if already exists
func (bm *BrowserManager) NewTab(tabID string) (*BrowserTab, error) {
	bm.mu.RLock()
	needsInit := bm.allocCtx == nil
	if existingTab, ok := bm.tabs[tabID]; ok {
		bm.mu.RUnlock()
		return existingTab, nil
	}
	bm.mu.RUnlock()

	if needsInit {
		if err := bm.Initialize(); err != nil {
			return nil, err
		}
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.allocCtx == nil {
		return nil, fmt.Errorf("browser not initialized")
	}

	tabCtx, tabCancel := chromedp.NewContext(bm.allocCtx)

	tab := &BrowserTab{
		ID:      tabID,
		Ctx:     tabCtx,
		Cancel:  tabCancel,
		History: make([]string, 0),
	}

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

// NavigateAndGetContent navigates to URL and gets page content in single call
func (bm *BrowserManager) NavigateAndGetContent(tabID string, url string) (string, string, error) {
	bm.mu.RLock()
	tab, ok := bm.tabs[tabID]
	bm.mu.RUnlock()

	if !ok {
		return "", "", fmt.Errorf("tab not found: %s", tabID)
	}

	var title string
	var text string

	err := chromedp.Run(tab.Ctx,
		chromedp.Navigate(url),
		chromedp.Title(&title),
		chromedp.Text("body", &text),
	)

	if err != nil {
		return "", "", fmt.Errorf("failed to navigate and get content: %w", err)
	}

	// Install JS dialog interceptor so alert/confirm/prompt never block the session
	_ = bm.InstallDialogInterceptor(tabID)

	bm.mu.Lock()
	tab.URL = url
	tab.History = append(tab.History, url)
	tab.Title = title
	bm.mu.Unlock()

	return title, text, nil
}

// Click clicks an element by selector
func (bm *BrowserManager) Click(tabID string, selector string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(1000*time.Millisecond),
	)

	if err != nil {
		return err
	}

	jsScript := fmt.Sprintf(`
		(function() {
			var element = document.querySelector('%s');
			if (!element) {
				return 'Element not found';
			}
			element.scrollIntoView({ behavior: 'smooth', block: 'center' });
			return new Promise(function(resolve) {
				setTimeout(function() {
					try {
						element.click();
						resolve('Click successful');
					} catch(e) {
						resolve('Click error: ' + e.message);
					}
				}, 500);
			});
		})()
	`, selector)

	result, err := bm.ExecuteJS(tabID, jsScript)
	if err != nil {
		return err
	}

	resultStr := fmt.Sprintf("%v", result)
	if resultStr == "Element not found" {
		return fmt.Errorf("element not found: %s", selector)
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Type types text into an element
func (bm *BrowserManager) Type(tabID string, selector string, text string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx, chromedp.SetValue(selector, text))
}

// Scroll scrolls the page
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

// ScrollToElement scrolls to an element
func (bm *BrowserManager) ScrollToElement(tabID string, selector string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	script := fmt.Sprintf(`
		var element = document.querySelector('%s');
		if (element) {
			element.scrollIntoView({ behavior: 'smooth', block: 'center' });
		}
	`, selector)

	return chromedp.Run(ctx, chromedp.EvaluateAsDevTools(script, nil))
}

// Back goes back in history
func (bm *BrowserManager) Back(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.history.back()", nil),
	)
}

// ExecuteJS executes JavaScript
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

// GetConsoleLogs gets console logs
func (bm *BrowserManager) GetConsoleLogs(tabID string) ([]string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var logs []string
	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`
			console.logs = [];
			var originalLog = console.log;
			console.log = function() {
				console.logs.push([...arguments].join(' '));
				originalLog.apply(console, arguments);
			};
			console.logs;
		`, &logs),
	)

	return logs, err
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

// GetImages gets all images on the page
func (bm *BrowserManager) GetImages(tabID string) ([]string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	var images []string
	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`Array.from(document.images).map(img => img.src)`, &images),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get images: %w", err)
	}

	return images, nil
}

// Forward navigates forward in history
func (bm *BrowserManager) Forward(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.history.forward()", nil),
	)
}

// Refresh refreshes the current page
func (bm *BrowserManager) Refresh(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools("window.location.reload()", nil),
	)
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

// WaitForLoad waits for the page to finish loading
func (bm *BrowserManager) WaitForLoad(tabID string, timeout time.Duration) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, timeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
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

	return map[string]interface{}{
		"title":       title,
		"url":         url,
		"ready_state": readyState,
		"history_len": len(tab.History),
	}, nil
}

// ClearCache clears browser cache and cookies
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
		`, nil),
	)
}

// GetCookies gets all cookies for the current page
func (bm *BrowserManager) GetCookies(tabID string) ([]map[string]interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 30*time.Second)
	defer cancel()

	var cookies []map[string]interface{}
	err := chromedp.Run(ctx,
		chromedp.EvaluateAsDevTools(`document.cookie.split(';').map(c => {
			const [name, value] = c.trim().split('=');
			return { name, value };
		})`, &cookies),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get cookies: %w", err)
	}

	return cookies, nil
}

// ============================================================================
// Keyboard input
// ============================================================================

// PressKey presses a named key (Enter, Tab, Escape, ArrowUp, F5, ...) or types
// arbitrary text into the currently focused element of the page. Named keys use
// native CDP key events (so form submission on Enter works), not synthetic JS events.
func (bm *BrowserManager) PressKey(tabID string, key string, times int) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	keys := normalizeKeyName(key)
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
		actions = append(actions, chromedp.KeyEvent(keys))
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, actions...); err != nil {
		return fmt.Errorf("failed to press key %q: %w", key, err)
	}
	return nil
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
// Native JS dialog interception (alert / confirm / prompt)
// ============================================================================

// dialogInterceptorScript overrides window.alert/confirm/prompt so that native
// dialogs are recorded into window.__magicDialogs instead of blocking the
// headless automation session. Responses can be pre-set via
// window.__magicDialogResponses (keyed by dialog type).
const dialogInterceptorScript = `
(function() {
	if (window.__magicDialogsInstalled) return;
	window.__magicDialogsInstalled = true;
	window.__magicDialogs = window.__magicDialogs || [];
	window.__magicDialogResponses = window.__magicDialogResponses || {};
	function record(type, message, defaultValue) {
		var entry = { type: type, message: String(message), timestamp: new Date().toISOString() };
		if (defaultValue !== undefined) entry.default_value = defaultValue;
		window.__magicDialogs.push(entry);
	}
	window.alert = function(msg) {
		record('alert', msg);
		return true;
	};
	window.confirm = function(msg) {
		record('confirm', msg);
		var resp = window.__magicDialogResponses['confirm'];
		return resp === undefined ? false : (resp === true || resp === 'true');
	};
	window.prompt = function(msg, defaultValue) {
		record('prompt', msg, defaultValue);
		var resp = window.__magicDialogResponses['prompt'];
		return resp === undefined ? (defaultValue !== undefined ? defaultValue : null) : resp;
	};
})()
`

// InstallDialogInterceptor injects the alert/confirm/prompt interceptor into the
// current page. It is idempotent and safe to call after every navigation.
func (bm *BrowserManager) InstallDialogInterceptor(tabID string) error {
	_, err := bm.ExecuteJS(tabID, dialogInterceptorScript)
	if err != nil {
		return fmt.Errorf("failed to install dialog interceptor: %w", err)
	}
	return nil
}

// GetPendingDialogs returns all dialogs captured by the interceptor so far.
func (bm *BrowserManager) GetPendingDialogs(tabID string) ([]map[string]interface{}, error) {
	result, err := bm.ExecuteJS(tabID, `JSON.stringify(window.__magicDialogs || [])`)
	if err != nil {
		return nil, fmt.Errorf("failed to read pending dialogs: %w", err)
	}

	return parseDialogsResult(result)
}

// parseDialogsResult converts the raw ExecuteJS result into dialog entries.
func parseDialogsResult(result interface{}) ([]map[string]interface{}, error) {
	switch v := result.(type) {
	case string:
		var dialogs []map[string]interface{}
		if err := json.Unmarshal([]byte(v), &dialogs); err != nil {
			return nil, fmt.Errorf("failed to parse dialogs: %w", err)
		}
		return dialogs, nil
	case []interface{}:
		dialogs := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				dialogs = append(dialogs, m)
			}
		}
		return dialogs, nil
	case nil:
		return []map[string]interface{}{}, nil
	default:
		return nil, fmt.Errorf("unexpected dialog result type: %T", result)
	}
}

// ClearDialogs clears all recorded pending dialogs.
func (bm *BrowserManager) ClearDialogs(tabID string) error {
	_, err := bm.ExecuteJS(tabID, `window.__magicDialogs = []; true`)
	if err != nil {
		return fmt.Errorf("failed to clear dialogs: %w", err)
	}
	return nil
}

// SetDialogResponse pre-sets the response that the interceptor will return for
// future dialogs of the given type ("confirm" or "prompt"). For confirm, use
// "true"/"false". For prompt, use the text value to return.
func (bm *BrowserManager) SetDialogResponse(tabID string, dialogType string, response string) error {
	typ, err := json.Marshal(dialogType)
	if err != nil {
		return fmt.Errorf("invalid dialog type: %w", err)
	}
	resp, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("invalid response: %w", err)
	}

	script := fmt.Sprintf(`window.__magicDialogResponses = window.__magicDialogResponses || {}; window.__magicDialogResponses[%s] = %s; true`, typ, resp)
	if _, err := bm.ExecuteJS(tabID, script); err != nil {
		return fmt.Errorf("failed to set dialog response: %w", err)
	}
	return nil
}
