package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
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
		chromedp.Flag("log-level", 3),
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
