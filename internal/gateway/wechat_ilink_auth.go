package gateway

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// ILinkLoginOpts configures the interactive QR login flow.
type ILinkLoginOpts struct {
	// Base URL for the iLink API (default: https://ilinkai.weixin.qq.com/)
	BaseURL string
	// Bot type (default: "3")
	BotType string
	// Proxy URL (optional)
	Proxy string
	// Timeout for the login flow (default: 5 minutes)
	Timeout time.Duration
	// Silent mode - if true, don't print QR code to terminal (for Web QR mode)
	Silent bool
}

// PerformILinkLogin starts the WeChat QR login flow and blocks until login is
// successful or times out. It prints a QR code to the terminal for the user to scan.
//
// Returns the BotToken, IlinkUserID, IlinkBotID, and BaseUrl on success.
func PerformILinkLogin(ctx context.Context, opts ILinkLoginOpts) (botToken, userID, botID, baseURL string, err error) {
	if opts.BaseURL == "" {
		opts.BaseURL = ilinkDefaultBaseURL
	}
	if opts.BotType == "" {
		opts.BotType = ilinkDefaultBotType
	}
	if opts.Timeout == 0 {
		opts.Timeout = ilinkAuthDefaultTimeout
	}

	// Create API client without token (QR endpoints don't need auth)
	api, err := NewILinkAPIClient(opts.BaseURL, "", opts.Proxy)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to create api client: %w", err)
	}
	pollAPI := api

	log.Info("[WeChat-iLink] Requesting QR code for login...")

	qrResp, err := api.GetQRCode(ctx, opts.BotType)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to get QR code: %w", err)
	}

	// Only print QR code to terminal in non-silent mode
	if !opts.Silent {
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════╗")
		fmt.Println("║  📱 微信扫码登录 / WeChat QR Login                   ║")
		fmt.Println("╚══════════════════════════════════════════════════════╝")
		fmt.Println()
		fmt.Println("Instructions:")
		fmt.Println("  1. Open WeChat on your phone")
		fmt.Println("  2. Tap '+' → 'Scan'")
		fmt.Println("  3. Scan the QR code")
		fmt.Println("  4. Confirm login on your phone")
		fmt.Println()

		// Print QR code to terminal using QR code URL
		// The QR code image is available as a data URL (qrcode_img_content)
		if qrResp.QrcodeImgContent != "" {
			// If it's a data URL (starts with data:image), print the URL for scanning
			fmt.Println("QR Code URL:", qrResp.QrcodeImgContent)
			fmt.Println()
			fmt.Println("💡 Tips:")
			fmt.Println("   • Copy the URL above and open it in a browser to scan")
			fmt.Println("   • Or right-click and copy image address if using a terminal with image support")
			fmt.Println()
		} else {
			fmt.Println("QR Code Key:", qrResp.Qrcode)
		}

		fmt.Println("⏳ Waiting for scan... (timeout:", opts.Timeout, ")")
		fmt.Println()
	} else {
		log.Info("[WeChat-iLink] QR login started in silent mode (Web QR)")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	pollTicker := time.NewTicker(ilinkAuthPollInterval)
	defer pollTicker.Stop()

	scannedPrinted := false

	for {
		select {
		case <-timeoutCtx.Done():
			return "", "", "", "", fmt.Errorf("login timeout (%v)", opts.Timeout)

		case <-pollTicker.C:
			statusResp, err := pollAPI.GetQRCodeStatus(timeoutCtx, qrResp.Qrcode)
			if err != nil {
				// Temporary error, retry
				continue
			}

			switch statusResp.Status {
			case "wait":
				// Still waiting for scan

			case "scaned":
				if !scannedPrinted && !opts.Silent {
					fmt.Println("👀 二维码已扫描！请在手机上确认登录...")
					fmt.Println("   QR code scanned! Please confirm login on your phone...")
					scannedPrinted = true
				}

			case "confirmed":
				if statusResp.BotToken == "" || statusResp.IlinkBotID == "" {
					return "", "", "", "",
						fmt.Errorf("login confirmed but missing bot_token or ilink_bot_id")
				}

				baseURL := statusResp.Baseurl
				if baseURL == "" {
					baseURL = opts.BaseURL
				}

				log.Infof("[WeChat-iLink] ✅ Login successful! BotID: %s",
					statusResp.IlinkBotID)

				if !opts.Silent {
					fmt.Println()
					fmt.Println("=======================================================")
					fmt.Println("✅ 登录成功！(Login successful!)")
					fmt.Printf("   Bot Token: %s\n", statusResp.BotToken)
					fmt.Printf("   Bot ID: %s\n", statusResp.IlinkBotID)
					fmt.Printf("   User ID: %s\n", statusResp.IlinkUserID)
					fmt.Printf("   API Base: %s\n", baseURL)
					fmt.Println("=======================================================")
					fmt.Println()
				}

				return statusResp.BotToken, statusResp.IlinkUserID,
					statusResp.IlinkBotID, baseURL, nil

			case "scaned_but_redirect":
				if statusResp.RedirectHost == "" {
					log.Warn("[WeChat-iLink] scaned_but_redirect without redirect_host; continuing")
					continue
				}
				nextBaseURL := "https://" + statusResp.RedirectHost + "/"
				nextAPI, nextErr := NewILinkAPIClient(nextBaseURL, "", opts.Proxy)
				if nextErr != nil {
					log.Warnf("[WeChat-iLink] Failed to switch polling host to %s: %v",
						nextBaseURL, nextErr)
					continue
				}
				pollAPI = nextAPI
				log.Infof("[WeChat-iLink] Switched QR polling host to %s", nextBaseURL)

			case "expired":
				fmt.Println("⏰ 二维码已过期，请重新开始登录流程")
				return "", "", "", "", fmt.Errorf("QR code expired, please restart login")

			default:
				log.Warnf("[WeChat-iLink] Unknown QR code status: %s", statusResp.Status)
			}
		}
	}
}

// PerformILinkLoginTerminal performs QR login with terminal QR rendering support.
// If the terminal supports it, it will render the QR code as ASCII art.
// Otherwise, it provides the QR URL for manual scanning.
func PerformILinkLoginTerminal(ctx context.Context, opts ILinkLoginOpts) (botToken, userID, botID, baseURL string, err error) {
	if opts.BaseURL == "" {
		opts.BaseURL = ilinkDefaultBaseURL
	}
	if opts.BotType == "" {
		opts.BotType = ilinkDefaultBotType
	}
	if opts.Timeout == 0 {
		opts.Timeout = ilinkAuthDefaultTimeout
	}

	api, err := NewILinkAPIClient(opts.BaseURL, "", opts.Proxy)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to create api client: %w", err)
	}
	pollAPI := api

	log.Info("[WeChat-iLink] Requesting QR code...")

	qrResp, err := api.GetQRCode(ctx, opts.BotType)
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to get QR code: %w", err)
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║  微信扫码登录 / WeChat QR Login                       ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("📱 QR Code URL (open in browser to scan):")
	fmt.Println()
	fmt.Println("  " + qrResp.QrcodeImgContent)
	fmt.Println()
	fmt.Println("⏳ Waiting for scan... (timeout:", opts.Timeout, ")")
	fmt.Println()

	// Save QR code as HTML file for easy opening
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>WeChat Bot Login</title>
	<meta charset="utf-8">
	<style>
		body { display: flex; justify-content: center; align-items: center;
			height: 100vh; font-family: sans-serif; flex-direction: column; }
		img { max-width: 400px; border: 2px solid #07c160; border-radius: 8px; padding: 8px; }
		h2 { color: #07c160; }
	</style>
</head>
<body>
	<h2>📱 Scan with WeChat</h2>
	<img src="%s" alt="QR Code">
	<p>Open WeChat → Scan QR Code → Confirm Login</p>
</body>
</html>`, qrResp.QrcodeImgContent)

	htmlFile := "wechat_login.html"
	if err := os.WriteFile(htmlFile, []byte(htmlContent), 0644); err == nil {
		fmt.Printf("💡 QR page saved to: %s (open in browser)\n", htmlFile)
	} else {
		// Save as simple text file with just the URL
		_ = os.WriteFile("wechat_login_url.txt",
			[]byte(qrResp.QrcodeImgContent+"\n"), 0644)
		fmt.Println("💡 QR URL saved to: wechat_login_url.txt")
	}

	fmt.Println()

	timeoutCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	pollTicker := time.NewTicker(ilinkAuthPollInterval)
	defer pollTicker.Stop()

	scannedPrinted := false

	for {
		select {
		case <-timeoutCtx.Done():
			return "", "", "", "", fmt.Errorf("login timeout (%v)", opts.Timeout)

		case <-pollTicker.C:
			statusResp, err := pollAPI.GetQRCodeStatus(timeoutCtx, qrResp.Qrcode)
			if err != nil {
				continue
			}

			switch statusResp.Status {
			case "wait":

			case "scaned":
				if !scannedPrinted {
					fmt.Println("👀 QR scanned! Confirm on your phone...")
					scannedPrinted = true
				}

			case "confirmed":
				if statusResp.BotToken == "" || statusResp.IlinkBotID == "" {
					return "", "", "", "",
						fmt.Errorf("confirmed but missing credentials")
				}
				baseURL = statusResp.Baseurl
				if baseURL == "" {
					baseURL = opts.BaseURL
				}
				fmt.Println()
				fmt.Println("✅ LOGIN SUCCESSFUL!")
				fmt.Printf("   Bot Token: %s\n", statusResp.BotToken)
				return statusResp.BotToken, statusResp.IlinkUserID,
					statusResp.IlinkBotID, baseURL, nil

			case "scaned_but_redirect":
				if statusResp.RedirectHost != "" {
					nextBaseURL := "https://" + statusResp.RedirectHost + "/"
					nextAPI, err := NewILinkAPIClient(nextBaseURL, "", opts.Proxy)
					if err == nil {
						pollAPI = nextAPI
					}
				}

			case "expired":
				fmt.Println("⏰ QR code expired, please re-run login")
				return "", "", "", "", fmt.Errorf("QR code expired")

			default:
				log.Warnf("[WeChat-iLink] Unknown QR status: %s", statusResp.Status)
			}
		}
	}
}
