#!/usr/bin/env python3
"""
go-magic Web Wrapper - 跨平台 Web 套壳应用
基于 PyWebView 实现，打包为独立可执行文件
"""

import os
import sys
import threading
import webbrowser
import argparse
import logging
from pathlib import Path

# PyWebView 导入
try:
    import webview
    HAS_PYWEBVIEW = True
except ImportError:
    HAS_PYWEBVIEW = False
    print("Warning: pywebview not installed. Running in browser mode.")

import http.server
import socketserver
import urllib.parse

logger = logging.getLogger(__name__)


class GoMagicServerHandler(http.server.SimpleHTTPRequestHandler):
    """自定义 HTTP 处理器"""
    
    def __init__(self, *args, directory=None, **kwargs):
        self.static_dir = directory
        super().__init__(*args, directory=directory, **kwargs)
    
    def do_GET(self):
        """处理 GET 请求"""
        if self.path == '/':
            self.path = '/index.html'
        elif self.path == '/api/health':
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status": "ok", "app": "go-magic-web"}')
            return
        return super().do_GET()
    
    def log_message(self, format, *args):
        """自定义日志格式"""
        logger.debug("%s - - [%s] %s" % (
            self.address_string(),
            self.log_date_time_string(),
            format % args
        ))


def start_server(port: int, directory: str) -> tuple:
    """启动本地 HTTP 服务器"""
    os.chdir(directory)
    
    with socketserver.TCPServer(("", port), GoMagicServerHandler) as httpd:
        httpd.allow_reuse_address = True
        logger.info(f"Server started at http://localhost:{port}")
        return httpd


def open_browser(url: str, delay: float = 1.0):
    """延迟打开浏览器"""
    import time
    time.sleep(delay)
    webbrowser.open(url)


class GoMagicWebApp:
    """GoMagic Web 套壳应用"""
    
    def __init__(self, api_url: str = "http://localhost:8642", 
                 port: int = 8648,
                 title: str = "go-magic AI Agent",
                 width: int = 1200,
                 height: int = 800,
                 webview: bool = True):
        self.api_url = api_url
        self.port = port
        self.title = title
        self.width = width
        self.height = height
        self.use_webview = webview and HAS_PYWEBVIEW
        self.server = None
        self.server_thread = None
    
    def get_static_dir(self) -> str:
        """获取静态文件目录"""
        # 优先使用打包后的静态文件
        if hasattr(sys, '_MEIPASS'):
            return os.path.join(sys._MEIPASS, 'web')
        
        # 开发模式使用本地文件
        base_dir = Path(__file__).parent
        return str(base_dir / 'web')
    
    def start(self):
        """启动应用"""
        static_dir = self.get_static_dir()
        
        if not os.path.exists(static_dir):
            logger.error(f"Static directory not found: {static_dir}")
            print(f"Error: Web files not found at {static_dir}")
            print("Please build the web app first: cd web && pnpm build")
            return False
        
        # 启动服务器
        self.server_thread = threading.Thread(
            target=lambda: start_server(self.port, static_dir),
            daemon=True
        )
        self.server_thread.start()
        
        url = f"http://localhost:{self.port}"
        
        if self.use_webview:
            self._start_webview(url)
        else:
            self._start_browser(url)
        
        return True
    
    def _start_webview(self, url: str):
        """使用 PyWebView 启动"""
        logger.info("Starting PyWebView...")
        
        # 创建窗口
        window = webview.create_window(
            self.title,
            url,
            width=self.width,
            height=self.height,
            resizable=True,
            fullscreen=False,
            min_size=(800, 600)
        )
        
        # 设置 JavaScript API
        def api_call(method: str, **kwargs):
            """JavaScript API 调用"""
            import json
            import urllib.request
            
            api_endpoint = f"{self.api_url}/api/{method}"
            
            try:
                data = json.dumps(kwargs).encode() if kwargs else None
                req = urllib.request.Request(
                    api_endpoint,
                    data=data,
                    headers={'Content-Type': 'application/json'}
                )
                with urllib.request.urlopen(req, timeout=10) as response:
                    return response.read().decode()
            except Exception as e:
                return json.dumps({'error': str(e)})
        
        # 启动
        webview.start(debug=False)
    
    def _start_browser(self, url: str):
        """使用系统浏览器启动"""
        logger.info("Starting in browser mode...")
        print(f"Opening {url} in your default browser...")
        webbrowser.open(url)
        
        # 保持运行
        try:
            input("Press Enter to stop the server...")
        except KeyboardInterrupt:
            pass


def main():
    """主入口"""
    parser = argparse.ArgumentParser(
        description='go-magic Web Wrapper - AI Agent Web Interface'
    )
    parser.add_argument(
        '--api',
        default=os.environ.get('GO_MAGIC_API', 'http://localhost:8642'),
        help='go-magic API URL (default: http://localhost:8642)'
    )
    parser.add_argument(
        '--port',
        type=int,
        default=int(os.environ.get('GO_MAGIC_PORT', '8648')),
        help='Web server port (default: 8648)'
    )
    parser.add_argument(
        '--title',
        default='go-magic AI Agent',
        help='Window title'
    )
    parser.add_argument(
        '--no-webview',
        action='store_true',
        help='Use system browser instead of native window'
    )
    parser.add_argument(
        '--width',
        type=int,
        default=1200,
        help='Window width (default: 1200)'
    )
    parser.add_argument(
        '--height',
        type=int,
        default=800,
        help='Window height (default: 800)'
    )
    parser.add_argument(
        '--debug',
        action='store_true',
        help='Enable debug mode'
    )
    
    args = parser.parse_args()
    
    # 配置日志
    level = logging.DEBUG if args.debug else logging.INFO
    logging.basicConfig(
        level=level,
        format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
    )
    
    # 创建并启动应用
    app = GoMagicWebApp(
        api_url=args.api,
        port=args.port,
        title=args.title,
        width=args.width,
        height=args.height,
        webview=not args.no_webview
    )
    
    if app.start():
        print(f"\ngo-magic Web Interface started!")
        print(f"API: {args.api}")
        print(f"Web: http://localhost:{args.port}")
    else:
        sys.exit(1)


if __name__ == '__main__':
    main()
