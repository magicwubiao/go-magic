# go-magic Web Wrapper

跨平台 Web 套壳应用，基于 PyWebView 实现，可打包为独立可执行文件。

## 安装依赖

```bash
pip install pywebview>=4.0
```

## 使用方法

### 方式一：使用 PyWebView 原生窗口

```bash
python web_wrapper.py
```

### 方式二：使用系统浏览器

```bash
python web_wrapper.py --no-webview
```

### 自定义参数

```bash
# 指定端口
python web_wrapper.py --port 9000

# 指定 API 地址
python web_wrapper.py --api http://localhost:8080

# 自定义窗口大小
python web_wrapper.py --width 1400 --height 900

# 调试模式
python web_wrapper.py --debug
```

## 打包为可执行文件

### 使用 PyInstaller

```bash
pip install pyinstaller

pyinstaller --onefile --add-data "web:web" web_wrapper.py
```

### Linux/macOS

```bash
pyinstaller --onefile \
  --add-data "web:web" \
  --hidden-import=webview \
  -w web_wrapper.py
```

### Windows

```bash
pyinstaller --onefile ^
  --add-data "web;web" ^
  --hidden-import=webview ^
  -w web_wrapper.py
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| GO_MAGIC_API | http://localhost:8642 | go-magic API 地址 |
| GO_MAGIC_PORT | 8648 | Web 服务器端口 |

## 打包后的文件结构

```
dist/
├── web_wrapper.exe     # Windows 可执行文件
├── web_wrapper         # Linux/macOS 可执行文件
└── web/                # 静态文件目录
    ├── index.html
    ├── assets/
    └── ...
```
