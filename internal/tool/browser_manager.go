package tool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// BrowserManager manages Chrome browser instances using chromedp
type BrowserManager struct {
	mu             sync.RWMutex
	allocCtx       context.Context
	allocCancel    context.CancelFunc
	tabs           map[string]*BrowserTab
	defaultTimeout time.Duration
}

// BrowserTab represents a single browser tab
type BrowserTab struct {
	ID      string
	Ctx     context.Context
	Cancel  context.CancelFunc
	URL     string
	Title   string
	History []string
}

// NewBrowserManager creates a new browser manager
func NewBrowserManager() *BrowserManager {
	return &BrowserManager{
		tabs:           make(map[string]*BrowserTab),
		defaultTimeout: 30 * time.Second,
	}
}

// Initialize initializes the browser allocator
func (bm *BrowserManager) Initialize() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.allocCtx != nil {
		return nil // Already initialized
	}

	// Create allocator context (headless mode)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	bm.allocCtx = allocCtx
	bm.allocCancel = allocCancel

	return nil
}

// Close closes the browser manager and all tabs
func (bm *BrowserManager) Close() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Close all tabs
	for _, tab := range bm.tabs {
		if tab.Cancel != nil {
			tab.Cancel()
		}
	}
	bm.tabs = make(map[string]*BrowserTab)

	// Cancel allocator
	if bm.allocCancel != nil {
		bm.allocCancel()
		bm.allocCtx = nil
		bm.allocCancel = nil
	}
}

// NewTab creates a new browser tab
func (bm *BrowserManager) NewTab(tabID string) (*BrowserTab, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.allocCtx == nil {
		if err := bm.Initialize(); err != nil {
			return nil, err
		}
	}

	// Create new tab context
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

// Navigate navigates to a URL
func (bm *BrowserManager) Navigate(tabID string, url string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
	)

	if err != nil {
		return fmt.Errorf("failed to navigate: %w", err)
	}

	// Update tab info
	tab.URL = url
	tab.History = append(tab.History, url)

	// Get title
	var title string
	chromedp.Run(ctx, chromedp.Title(&title))
	tab.Title = title

	return nil
}

// Click clicks an element by selector
func (bm *BrowserManager) Click(tabID string, selector string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.Click(selector, chromedp.NodeVisible),
	)
}

// Type types text into an element
func (bm *BrowserManager) Type(tabID string, selector string, text string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.SendKeys(selector, text, chromedp.NodeVisible),
	)
}

// Scroll scrolls the page
func (bm *BrowserManager) Scroll(tabID string, x, y int64) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.Evaluate(fmt.Sprintf(`window.scrollTo(%d, %d);`, x, y), nil),
	)
}

// ScrollToElement scrolls to a specific element
func (bm *BrowserManager) ScrollToElement(tabID string, selector string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.ScrollIntoView(selector, chromedp.NodeVisible),
	)
}

// Back goes back in history
func (bm *BrowserManager) Back(tabID string) error {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	if len(tab.History) < 2 {
		return fmt.Errorf("no history to go back")
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	return chromedp.Run(ctx,
		chromedp.NavigateBack(),
	)
}

// ExecuteJS executes JavaScript and returns the result
func (bm *BrowserManager) ExecuteJS(tabID string, script string) (interface{}, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return nil, fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	var result interface{}
	err := chromedp.Run(ctx,
		chromedp.Evaluate(script, &result),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	return result, nil
}

// GetPageContent gets the page HTML content
func (bm *BrowserManager) GetPageContent(tabID string) (string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return "", fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
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

// GetPageText gets the page text content
func (bm *BrowserManager) GetPageText(tabID string) (string, error) {
	tab, ok := bm.GetTab(tabID)
	if !ok {
		return "", fmt.Errorf("tab not found: %s", tabID)
	}

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
	defer cancel()

	var text string
	err := chromedp.Run(ctx,
		chromedp.Text("body", &text, chromedp.NodeVisible),
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

	ctx, cancel := context.WithTimeout(tab.Ctx, bm.defaultTimeout)
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

// Global browser manager instance
var (
	globalBrowserManager *BrowserManager
	browserManagerOnce   sync.Once
)

// GetBrowserManager returns the global browser manager instance
func GetBrowserManager() *BrowserManager {
	browserManagerOnce.Do(func() {
		globalBrowserManager = NewBrowserManager()
	})
	return globalBrowserManager
}
