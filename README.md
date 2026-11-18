# Formigo

全平台本地离线文件格式转换工具，采用 **Go** 语言与 **[MyGo](https://github.com/egoist/mygo)** 原生 GPU UI 框架构建（纯 Go、零 CGO、无 WebView，单二进制跨平台运行）。

## 架构特点

- **MyGo 原生 GPU 界面**：基于 `github.com/egoist/mygo/ui` 直接在 Metal (macOS)、Direct3D 11 (Windows)、OpenGL (Linux) 上绘制界面，秒开低内存，支持系统文件拖拽、深浅色外观、中英双语实时切换。
- **双层转换引擎调度**：
  1. **纯 Go 内置引擎 (`builtin`)**：零外部依赖开箱即用，支持常见图片/矢量 SVG/多尺寸 ICO、PDF 合并/拆分/AES-256 加解密/内嵌图提取/图文提取、Excel (`XLSX`) 与结构化数据 (`CSV/TSV/JSON/YAML/TOML/XML`)、办公与电子书 (`DOCX/EPUB/Markdown/HTML/TXT`)、字幕 (`SRT/VTT/ASS`) 以及归档 (`ZIP/TAR/TAR.GZ`)。
  2. **可选外部工具增强**：自动探测系统或便携 `bin/` 目录下的 **FFmpeg**（实时解析转码进度百分比）、**LibreOffice**（独立临时 Profile 并发安全转换 Office/WPS/PPT/PDF）、**Pandoc**、**Poppler (`pdftoppm`)**、**macOS `sips`** 与 **Tesseract OCR**。
- **工程安全保障**：
  - **原子交付与魔数校验**：所有转换先写入独立会话暂存目录，校验文件头签名与非空完整性后再原子落盘，退出自动清理残留。
  - **防覆盖保护与偏好记忆**：默认采用 `file (1).ext` 自动递增命名防止误覆盖源文件；按源扩展名自动记住上次选择的目标格式。
  - **并发任务池与随时取消**：支持多 Worker 并行队列、单任务/全队列即时取消 (`context.Context`)，以及多图转 PDF / PDF 合并 / 多文件打包的自由排序合并工坊。
- **桌面 GUI + 无头 CLI 双模式**：同一个二进制既可双击启动桌面界面，也可在终端或自动化脚本中直接执行批量转换与 JSON 输出。

---

## 支持格式一览

| 类别 | 输入格式 | 输出格式 | 驱动引擎 |
|---|---|---|---|
| **图片与图标** | `png`, `jpg`, `webp`, `gif`, `bmp`, `tiff`, `ico`, `svg` (`heic`, `avif`, `tga` 需 sips/FFmpeg) | `png`, `jpg`, `webp`, `bmp`, `tiff`, `gif`, `ico` (16–256 多尺寸), `pdf`, `mp4`, `webm`, `txt/md` (OCR) | 内置纯 Go + 可选 FFmpeg / sips / Tesseract |
| **PDF 文档** | `pdf` | `txt`, `md`, `html`, `png`, `jpg`, 按页拆分 (`zip`), AES-256 加密/解密 (`pdf`), 提取内嵌原图 (`zip`), 多文件合并 | 内置纯 Go (`pdfcpu` + CMap 解析) + Poppler/sips |
| **文字与办公** | `md`, `txt`, `html`, `docx` (`doc`, `odt`, `rtf`, `wps`, `pptx` 需 LibreOffice/Pandoc) | `docx`, `epub`, `pdf`, `html`, `md`, `txt` | 内置纯 Go + 可选 LibreOffice / Pandoc |
| **电子书** | `epub` | `md`, `txt`, `html`, `docx`, `pdf` | 内置纯 Go (EPUB 3.0) + 可选 LibreOffice |
| **电子表格** | `xlsx`, `xlsm`, `csv`, `tsv` (`xls`, `ods`, `et` 需 LibreOffice) | `xlsx`, `csv` (含 UTF-8 BOM), `tsv`, `json`, `yaml`, `md`, `html`, `pdf` | 内置纯 Go (`excelize` + GB18030 自动识别) |
| **结构化数据** | `json`, `yaml`, `toml`, `xml` | `json`, `yaml`, `toml`, `xml`, `csv`, `xlsx`, `md`, `html` | 内置纯 Go |
| **字幕文件** | `srt`, `vtt`, `ass`, `ssa` | `srt`, `vtt`, `ass`, `txt`, `json` | 内置纯 Go |
| **音频媒体** | `mp3`, `wav`, `flac`, `m4a`, `aac`, `ogg`, `opus`, `wma`, `aiff` | `mp3`, `wav`, `flac`, `m4a`, `aac`, `ogg`, `opus`, `wma` | FFmpeg |
| **视频媒体** | `mp4`, `mkv`, `mov`, `webm`, `avi`, `flv`, `wmv`, `m4v`, `ts` | `mp4` (H.264/H.265/AV1), `mkv`, `mov`, `webm`, `avi`, `gif` (调色板优化), 以及提取音频格式 | FFmpeg |
| **压缩归档** | `zip`, `tar`, `tar.gz` (含 Zip-Slip 路径穿越防护) | `zip`, `tar`, `tar.gz`, `pdf` (压缩包内图片自动合并 PDF) | 内置纯 Go |

---

## 快速开始

### 启动桌面原生界面

```bash
go run .
# 或编译可执行文件后运行
go build -o formigo .
./formigo
```

### 命令行 (CLI) 模式

```bash
# 查看当前机器就绪的内置与外部引擎
./formigo engines

# 查看当前激活的完整格式转换矩阵（支持 --json）
./formigo capabilities
./formigo capabilities --json

# 查询特定文件或扩展名可转换的目标格式
./formigo targets example.pdf

# 单个或批量转换
./formigo convert logo.svg --to ico -o app.ico
./formigo convert a.png b.png --to webp --quality 88 --output-dir ./out
./formigo convert video.mov --to mp4 --video-codec h265 --audio-bitrate 192k

# PDF 拆分 / AES-256 加密 / 解密 / 合并 / 多图合成 PDF
./formigo convert doc.pdf --pdf-op split --split-span 2 -d ./out
./formigo convert doc.pdf --pdf-op encrypt --password "your-secret" -o encrypted.pdf
./formigo images-to-pdf page1.png page2.jpg -o album.pdf
./formigo merge-pdfs part1.pdf part2.pdf -o full.pdf
```

### 全平台零 CGO 交叉编译

由于无需 CGO，可在任意一台开发机上直接交叉编译全平台二进制：

```bash
# macOS (Apple Silicon & Intel)
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o dist/formigo-darwin-arm64 .
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o dist/formigo-darwin-amd64 .

# Windows x64 (隐藏控制台窗口标志 -H=windowsgui 可选用于纯桌面发布)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/formigo-windows-amd64.exe .

# Linux x64 / arm64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/formigo-linux-amd64 .
```

### 便携引擎目录（可选）

可将外部工具放置在可执行文件同级的 `bin/` 目录（如 `bin/ffmpeg`、`bin/pandoc`、`bin/pdftoppm`、`bin/tesseract`），或在应用「偏好设置」页面指定自定义路径，Formigo 会自动探测并激活对应高阶转换通道。
# Formigo
