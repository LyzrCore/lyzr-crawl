package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

type BrowserInstance struct {
	browser  *rod.Browser
	launcher *launcher.Launcher
	inUse    bool
	lastUsed time.Time
}

type BrowserPool struct {
	instances []*BrowserInstance
	mu        sync.Mutex
	maxSize   int
}

func NewBrowserPool(maxSize int) *BrowserPool {
	return &BrowserPool{
		instances: make([]*BrowserInstance, 0, maxSize),
		maxSize:   maxSize,
	}
}

func (bp *BrowserPool) Get() (*rod.Browser, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	for _, instance := range bp.instances {
		if !instance.inUse {
			instance.inUse = true
			instance.lastUsed = time.Now()
			return instance.browser, nil
		}
	}

	if len(bp.instances) < bp.maxSize {
		browser, launcher, err := createBrowser()
		if err != nil {
			return nil, err
		}

		instance := &BrowserInstance{
			browser:  browser,
			launcher: launcher,
			inUse:    true,
			lastUsed: time.Now(),
		}
		bp.instances = append(bp.instances, instance)
		return browser, nil
	}

	return nil, fmt.Errorf("browser pool exhausted")
}

func (bp *BrowserPool) Release(browser *rod.Browser) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	for _, instance := range bp.instances {
		if instance.browser == browser {
			instance.inUse = false
			instance.lastUsed = time.Now()
			return
		}
	}
}

func (bp *BrowserPool) Shutdown() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	for _, instance := range bp.instances {
		if instance.browser != nil {
			instance.browser.Close()
		}
		if instance.launcher != nil {
			instance.launcher.Cleanup()
		}
	}
	bp.instances = nil
}

func createBrowser() (*rod.Browser, *launcher.Launcher, error) {
	l := launcher.New().
		Bin("/usr/bin/chromium").
		Headless(true).
		NoSandbox(true).
		Set("disable-dev-shm-usage").
		Set("disable-extensions").
		Set("disable-gpu").
		Set("disable-web-security")

	controlURL, err := l.Launch()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(controlURL)
	err = browser.Connect()
	if err != nil {
		l.Cleanup()
		return nil, nil, fmt.Errorf("failed to connect to browser: %v", err)
	}

	return browser, l, nil
}
