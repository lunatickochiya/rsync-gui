# rsync-tui

> 把 [rsync-gui](../README.zh-CN.md) 的 Qt 图形界面改造成 **OpenCode CLI 风格** 的全屏终端应用。
> 单一 Go 二进制，不需要 Qt / X11 / WSLg，在 WSL、SSH 和普通终端里都能直接使用。

`rsync-tui` 保留原版最有用的能力：源列表、目标目录、常用 rsync 选项、预设、
复制后校验、移动、进度/速度/ETA、日志、取消、Git 跟踪文件、设置 JSON 导入导出。
界面使用 OpenCode 默认暗色主题的配色（`#0a0a0a` / `#fab283`）、侧栏、快捷键提示和
命令行式输入框，全部操作都可以只用键盘完成。

```text
  ◆ rsync tui  /  传输设置                                                      rsync 3.5.0   WSL
────────────────────────────────────────────────────────────────────────────────────────────────────
█▀▀█ █▀▀ █ █ █▀▀█ █▀▀█        传输设置   复制                                          预设 自定义
█▄▄▀ ▀▀█ ▀▀█ █  █ █         ╭──────────────────────────────────────────────────────────────────────╮
▀ ▀▀ ▀▀▀ ▀▀▀ ▀  ▀ ▀▀▀▀      │ 目标  /mnt/d/backup                                            d 编辑 │
终端里的 rsync 传输面板      ╰──────────────────────────────────────────────────────────────────────╯

                              选项  40 项                                                        ↓
源  1                         传输 ───────────────────────────────────────────────────────────────
▌ 1 · /home/me/Photos         [x] 归档模式                                                      -a
                                [x] 递归目录                                                    -r
                                [x] 符号链接                                                    -l
                                [x] 权限                                                        -p
                                [x] 修改时间                                                    -t
                                [x] 所属组                                                      -g
                                [x] 所有者                                                      -o
                                [x] 设备与特殊文件                                              -D
                              [ ] 硬链接                                                        -H
                              [ ] ACL                                                           -A
                              [ ] 扩展属性                                                      -X
                              [ ] 压缩传输                                                      -z
                              进度与冲突 ─────────────────────────────────────────────────────────
                              ‹每文件› 进度显示                                         --progress
  a 添加源  A 批量            [x] 保留部分文件                                           --partial
                              ‹覆盖› 已存在文件                                                 -u
预设                          [ ] 删除目标多余文件                                        --delete
  自定义                      命令 v 查看  rsync -avhP -- /home/…/Photos /mnt/d/backup/
  p 选择预设
────────────────────────────────────────────────────────────────────────────────────────────────────
  a 添加 · A 批量 · e 编辑 · x 移除 · d 目标 · p 预设 · r 开始 · ? 帮助 · q 退出
```

运行界面（实际有蓝→桃色渐变进度条、速度曲线和分级日志颜色）：

```text
  ◆ rsync tui  /  复制 1/1                                                                                       00:04
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
   复制 1/1                                                                                                      00:04
  14%  8.0 MiB/s  ETA 0:00:13
  ████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
  已传输 17.96M  ·  8.01MB/s  ·  rsync-tui-src/big.bin
  速度 ██
  日志  5 行  ·  跟随    ↑↓ 滚动 · end 跟随
  15:22:01  › --- 复制 1/1 ---
  15:22:01  › $ rsync -avhP --bwlimit=8m -- /tmp/rsync-tui-src /tmp/rsync-tui-backup/
  15:22:01  · sending incremental file list
  15:22:01  · rsync-tui-src/
  15:22:01  · rsync-tui-src/big.bin

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  esc 取消   ↑↓ 滚动日志   end 跟随   ? 帮助
```

## 快速开始（WSL / Debian / Ubuntu）

运行时只需要 `rsync`；远程传输额外需要 `ssh`。

```bash
sudo apt install rsync openssh-client
```

构建需要 Go 1.24.2+：

```bash
cd tui
make                 # 生成 tui/bin/rsync-tui
make install         # 安装到 ~/.local/bin/rsync-tui
```

如果 `~/.local/bin` 不在 `PATH` 中：

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

也可以直接运行：

```bash
./bin/rsync-tui ~/Photos -d /mnt/d/backup
```

> WSL 建议使用 Windows Terminal。程序是纯 TUI，不需要 WSLg，也不依赖 Windows 字体；
> 中文文件名会按 UTF-8 原样显示。

## 用法

```bash
# 打开界面，预填源和目标
rsync-tui ~/Photos ~/Documents/report.pdf --destination /mnt/d/backup

# 使用预设（复制 / 复制并校验 / FAT-NAS / 移动 …）
rsync-tui --preset rsync-copy-verify ~/Photos -d /mnt/d/backup

# 载入 rsync-gui 兼容的设置文件
rsync-tui --config ~/rsync-mirror.json -d /mnt/d/backup ~/src

# 填好后立即开始（会删除文件时仍会二次确认）
rsync-tui --preset rsync-move-verify ~/Photos -d nas:/volume1/photos --run

# 只输出命令，不启动界面（不会创建目标目录）
rsync-tui --print-command ~/Photos -d /mnt/d/backup

# 预设列表
rsync-tui --list-presets
```

`-d/--destination`、`-p/--preset`、`-c/--config` 可以放在源路径前后，参数顺序不限。

## 快捷键

| 按键 | 作用 |
| --- | --- |
| `tab` / `shift+tab` | 在“源”和“选项”之间切换 |
| `s` / `o` | 直接跳到源 / 选项 |
| `↑↓` `j` `k` `pgup` `pgdown` `g` `G` | 移动光标 |
| `a` | 添加一个源（支持本地、`user@host:/path`、`sftp://host:port/path`） |
| `A` | 批量粘贴多个源，一行一个 |
| `e` / `x` / `c` | 编辑 / 移除 / 清空源 |
| `d` | 编辑目标；输入框中 `ctrl+n` 选择最近目标 |
| `tab` | 输入路径时补全文件和目录 |
| `space` | 开关选项；choice 类型循环取值 |
| `enter` | 编辑文本选项 / 开关选项 |
| `p` | 载入预设 |
| `t` | 快速开关试运行（`-n`） |
| `v` | 查看完整 rsync 命令（设置页或运行页） |
| `I` / `E` | 导入 / 导出 JSON 设置 |
| `r` | 开始传输 |
| `esc` | 运行中取消（二次确认）；结束后返回设置 |
| `?` | 完整帮助 |
| `q` | 退出 |

运行中 `↑↓` 滚动日志，`end` 回到自动跟随；`ctrl+c` 直接取消。

## 功能对照

| 原 GUI | TUI |
| --- | --- |
| 源列表、粘贴、命令行传入 | ✅ `a` / `A`，支持每行一个路径 |
| 本地、`user@host:path`、`sftp://user@host:port/path` | ✅ 保留 sftp 端口 |
| 目标目录自动创建 / 远程目标 | ✅ 输入 `d`；支持最近目标 |
| `-a` 及分项 `-r -l -p -t -g -o -D` | ✅ 归档开启时分项锁定，关闭后可单独选择 |
| `-H -A -X -v -h --partial -c -u --ignore-existing -z` | ✅ |
| `--bwlimit -e -i --exclude-from --link-dest` | ✅ 文本选项 |
| `--delete --remove-source-files -n` | ✅ 会删除文件时二次确认 |
| 进度：每文件 / 总体 / 关闭 | ✅ `‹每文件›` 循环切换 |
| 复制后校验（`--checksum` 二次 dry-run） | ✅ 校验前对本地目标 `syncfs`，尽量读磁盘而不是页缓存 |
| 移动并校验（校验通过后才删源） | ✅ 三次 rsync：复制 → 校验 → 删除 |
| 删除源中变空的目录 | ✅ 只 `rmdir`，非空目录保留并记录 |
| 预设（6 个内置 + 用户预设） | ✅ 兼容 `~/.local/share/rsync-gui/presets/*.json` |
| 设置 JSON 导入/导出、`--config` | ✅ 与 rsync-gui 同一格式 |
| 仅 Git 跟踪文件（含 `.git`） | ✅ 通过临时 `--files-from` 列表 |
| FAT 目标自动 `--modify-window=1` | ✅ Linux/WSL 下检测 vfat |
| 实时速度曲线、ETA、带时间戳日志 | ✅ |
| 取消 | ✅ 先 SIGINT，3 秒后 SIGKILL |
| 保留/恢复选项、最近目标 | ✅ `~/.config/rsync-tui/config.json` |
| Dolphin 右键、Plasma 通知、拖放 | ➖ 终端环境不适用 |

## 预设

内置预设与 rsync-gui 相同：

| 预设 ID | 名称 | 命令 |
| --- | --- | --- |
| `rsync-copy` | 复制 (-a) | `rsync -avhP` |
| `rsync-copy-verify` | 复制并校验 (-ac) | `rsync -avhPc` + 校验 |
| `rsync-copy-portable` | 复制到 FAT/NTFS/NAS (-rlt) | `rsync -rltvhP` |
| `rsync-move` | 移动 (-a) | `rsync -avhP --remove-source-files` + 清理空目录 |
| `rsync-move-verify` | 移动并校验 (-ac) | 复制 → 校验 → 删除源 |
| `rsync-move-portable` | 移动到 FAT/NTFS/NAS (-rlt) | `rsync -rltvhP --remove-source-files` |

搜索顺序（前面的覆盖后面的）：

1. `$XDG_DATA_HOME/rsync-gui/presets/`（默认 `~/.local/share/rsync-gui/presets/`）
2. `$XDG_DATA_DIRS/*/rsync-gui/presets/`
3. 程序目录的 `../share/rsync-gui/presets/`
4. 程序目录的 `presets/`
5. 当前目录的 `presets/`

内置预设编译进二进制，因此单独拷贝可执行文件也能用。

## 设置文件

TUI 的“导出设置 / 导入设置”与 rsync-gui 使用同一格式：

```json
{
    "format": "rsync-gui-settings",
    "version": 1,
    "name": "我的设置",
    "options": {
        "program": "rsync",
        "archive": true,
        "verifyAfter": true,
        "extraArguments": "--exclude='*.tmp'"
    }
}
```

缺失的键保留当前值，未知键忽略。`--config` 在启动时载入；`I` 在界面里导入；`E` 导出。
程序自己的记忆文件是 `~/.config/rsync-tui/config.json`，保存选项、源、目标和最近 10 个目标。

**安全设计**：`--delete`、`--remove-source-files`、`--remove-source-dirs` 和 `-n`
不会被带进下一次启动；它们只在当前会话或显式预设/设置文件中生效。每次会删除文件的运行
仍会先显示确认框。

## 远程传输

远程源或目标使用 rsync 的 ssh 传输。支持：

```text
user@host:/path
user@host:relative/path
sftp://user@host:2222/path
```

`sftp://` 的端口会转成 `-e "ssh -p 2222"`；SSH 密钥选项会追加 `-i <key>`。
**TUI 不处理密码提示**：请在 WSL 里配置 SSH 密钥或 `ssh-agent`（推荐 `ssh-copy-id`）。
SSH 的 stdin 被设为 `/dev/null`，密码主机不会把终端卡住，而是快速报“权限拒绝”。

不支持远程到远程的直接复制（rsync 本身只允许一端是远程）；先把一端落到本地。

## 已知限制

- 终端里没有拖放和系统文件选择器；用 `a`、`A`（粘贴）和 Tab 补全代替。
- 不处理 SSH 密码/主机密钥交互；请提前配置密钥和 `known_hosts`。未知主机密钥会失败。
- 非 UTF-8 文件名：Go 会原样传给 rsync；rsync 在 `C.UTF-8` 下可能以 `\#NNN` 形式显示，
  文件本身仍按字节复制。文件列表补全对这类名字只做字节级匹配。
- 远程到远程复制、rsync daemon（`rsync://`）不在支持范围内。
- 没有 Plasma 任务栏/通知区域进度，也没有 Dolphin 右键菜单。
- FAT 检测只覆盖 Linux/WSL 的 vfat；exFAT/NTFS 不需要 `--modify-window`。

## 开发

```bash
cd tui
make test        # 单元 + 真实 rsync 集成测试
make vet
make build
```

目录结构：

```text
tui/
  cmd/rsync-tui/         CLI 入口、参数解析
  internal/config/       选项、预设、设置 JSON、记忆配置
  internal/location/     本地/远程路径解析
  internal/rsync/        命令构建、传输计划、执行器、进度、校验、清理
  internal/tui/          Bubble Tea 界面（OpenCode 风格）
  presets/               内置预设（同时被 go:embed 打包）
```

集成测试会真实调用 `rsync`，覆盖复制+校验、移动+校验、校验差异和 Git 跟踪文件。
测试使用临时目录，不会碰用户数据。

## 许可

与原项目一致，MIT。见仓库根目录 [LICENSE](../LICENSE)。
