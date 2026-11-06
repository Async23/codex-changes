# 开发说明

## 构建与验证

```bash
git clone https://github.com/Async23/codex-changes.git
cd codex-changes
make check
make build
./dist/codex-changes --demo
make install
```

`make install` 默认写入 `~/.local/bin/codex-changes`，可通过 `INSTALL_DIR` 指定其他目录。运行环境为 macOS 或 Linux，开发需要 Go 1.25+。

## 模块

- `internal/codex`：管理一个 `codex app-server` 子进程，完成初始化、请求关联、取消和只读历史分页。
- `internal/changes`：解析成功的文件修改，按路径和移动记录汇总，生成统一差异及并排视图所需的数据。
- `internal/highlight`：使用 Chroma 解析补丁中的新内容，用 go-diff 比较词语，生成与终端主题和宽度无关的文字样式片段。
- `internal/tui`：项目、会话、目录、文件、修改记录和差异块的米勒列导航，连续时间线、键鼠交互、主题和终端尺寸适配。
- `internal/preferences`：原子保存界面偏好，不保存历史内容。
- `cmd/codex-changes`：交互入口和无界面的 `check` 命令。

历史读取只使用 `initialize`、`thread/list`、`thread/read`、`thread/turns/list`。客户端有方法白名单；未处理的服务端请求返回拒绝，不会自动批准。禁止增加对 Codex 私有 SQLite 或 JSONL 的依赖。

现代历史通过 `thread/turns/list` 请求 `itemsView=full`，处理所有分页。旧式历史使用 `thread/read(includeTurns=true)`；分页方法不存在时也会回退。只返回摘要、重复分页游标和读取失败必须明确报错，不能显示成完整扫描。子会话查询结果还要根据 `parentThreadId` 验证归属，避免旧服务端忽略筛选参数后读入无关会话。

协议字段以安装的 Codex CLI 生成的 schema 为准：

```bash
codex app-server generate-json-schema --experimental --out /tmp/codex-changes-schema
```

最初的本机兼容验证使用 Codex CLI `0.166.0`。修改协议适配时，应重新生成 schema，并用 `check` 验证真实历史；不要把真实会话、个人路径、命令输出或密钥放进测试与截图。

## 测试职责

- 文件汇总：非 Git 路径、跨目录文件、失败操作过滤、移动、重复条目、跨子会话时间顺序。
- 差异解析：完整新增与删除正文、多个 hunk、行号、并排对齐、终端控制序列清理。
- 高亮：语法上下文、替换词语、空白变化、中文与组合字符、过大输入回退，以及主题切换时的缓存有效性。
- 协议：本地合成服务端验证初始化、分页、并发请求、取消、错误处理和只读白名单。
- 界面：Yazi 风格全层键位、会话自动预览与过期扫描隔离、显式搜索、逐层进入与返回、目录与文件筛选、差异块内容隔离、预览滚动、连续时间线、键鼠操作、中文输入、窄屏、长路径、并排、换行及无溢出渲染。

`--demo` 使用源码中的合成数据，不连接 Codex。项目预览从演示终端画面生成。

## 差异渲染

视觉设计参考 Claude Code 的终端 diff：整行浅底色、词语深底色、单列行号，以及新增语法与删除普通文字的区分。实现使用 Go 和公开依赖，不运行 Claude Code。

处理分为两步。`highlight.Analyze` 在每个 hunk 中拼接新增与上下文，保留多行语法状态；词语比较保留空格，以字素簇为边界。`tui.codeRows` 再按终端单元格换行或裁切，为每段独立设置前景与背景，并用整行背景填充剩余宽度。单栏与并排共用这条渲染路径。

分析缓存只属于当前选中的文件，内容或文件路径变化时失效；调整宽度、主题和显示模式不需要重新分析。超过 256 KiB 的单个 hunk 跳过语法解析；过长或耗时的词语比较退回整行背景。所有回退都保留完整正文。历史没有包含的语法上下文不从当前磁盘补取。

更新浅色预览时，在独立终端运行图表演示，切到浅色，然后捕获（`-N` 保留行尾带底色的空格）：

```bash
codex-changes --demo --thread demo-diff --file overview.svg
tmux capture-pane -peN -t <demo-pane> | python3 scripts/render-preview.py --theme light > docs/preview.svg
```

## 贡献

欢迎提交复现步骤、界面改进或协议兼容补丁。请使用合成数据描述问题；提交前执行 `make check`。新增快捷键应同时检查终端和 tmux 是否截获该物理按键，更新使用说明。
