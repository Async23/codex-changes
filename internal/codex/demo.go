package codex

import (
	"context"
	"encoding/json"
	"fmt"
)

// Demo contains synthetic data only, and never starts Codex or reads local files.
type Demo struct{ threads []Thread }

func NewDemo() *Demo {
	makeTurn := func(id string, at int64, prompt string, changes []FileChange) Turn {
		user, _ := json.Marshal(map[string]any{"id": id + "-user", "type": "userMessage", "content": []map[string]string{{"type": "text", "text": prompt}}})
		patch, _ := json.Marshal(Item{ID: id + "-patch", Type: "fileChange", Status: "completed", Changes: changes})
		return Turn{ID: id, StartedAt: at, Status: "completed", ItemsView: "full", Items: []json.RawMessage{user, patch}}
	}
	camera := Thread{ID: "demo-camera", Name: "相机拍摄笔记", CWD: "/demo/camera", HistoryMode: "legacy", UpdatedAt: 1793525400}
	camera.Turns = []Turn{
		makeTurn("camera-1", 1793514600, "整理拍摄笔记，补上读出时间的说明。", []FileChange{
			{Path: "notes/读出时间.md", Kind: ChangeKind{Type: "add"}, Diff: "# 读出时间\n\n读出时间取决于画幅和采样模式。\n\n## 测量方法\n使用稳定频率的光源。\n记录拍摄设置。\n"},
			{Path: "scripts/measure.py", Kind: ChangeKind{Type: "add"}, Diff: "from pathlib import Path\n\ndef measure(frame):\n    return frame.height / frame.frequency\n\nprint(measure(frame))\n"},
			{Path: "export/settings.json", Kind: ChangeKind{Type: "add"}, Diff: "{\n  \"format\": \"markdown\",\n  \"include_sources\": true\n}\n"},
		}),
		makeTurn("camera-2", 1793521800, "把不同模式分开说明，并记录引用来源，方便以后复核。", []FileChange{
			{Path: "notes/读出时间.md", Kind: ChangeKind{Type: "update"}, Diff: "@@ -1,7 +1,12 @@\n # 读出时间\n \n-读出时间取决于画幅和采样模式。\n+读出时间需要结合画幅、分辨率和采样模式一起比较。\n+同一台相机的不同模式不能直接共用一个测量值。\n \n ## 测量方法\n 使用稳定频率的光源。\n 记录拍摄设置。\n+\n+## 引用来源\n+每条数据保留来源链接和测量条件。\n+缺少测量条件的数据单独标记为待核验。\n"},
			{Path: "scripts/measure.py", Kind: ChangeKind{Type: "update"}, Diff: "@@ -3,2 +3,4 @@\n def measure(frame):\n+    if frame.frequency <= 0:\n+        raise ValueError(\"frequency must be positive\")\n     return frame.height / frame.frequency\n"},
		}),
	}
	child := Thread{ID: "demo-camera-child", ParentID: camera.ID, Name: "导出格式检查", AgentName: "Reviewer", CWD: camera.CWD, HistoryMode: "legacy"}
	child.Turns = []Turn{makeTurn("child-1", 1793525400, "补齐导出格式，并去掉旧临时说明。", []FileChange{
		{Path: "export/settings.json", Kind: ChangeKind{Type: "update"}, Diff: "@@ -1,4 +1,5 @@\n {\n   \"format\": \"markdown\",\n-  \"include_sources\": true\n+  \"include_sources\": true,\n+  \"encoding\": \"utf-8\"\n }\n"},
		{Path: "/demo/scratch/old-notes.txt", Kind: ChangeKind{Type: "delete"}, Diff: "临时测量记录\n待合并到正式笔记\n"},
	})}
	website := Thread{ID: "demo-website", Name: "网站导航调整", CWD: "/demo/website", HistoryMode: "legacy", UpdatedAt: 1793439000}
	website.Turns = []Turn{makeTurn("web-1", 1793439000, "添加一个文档入口。", []FileChange{{Path: "src/navigation.ts", Kind: ChangeKind{Type: "update"}, Diff: "@@ -1,3 +1,4 @@\n export const links = [\n   { label: 'Home', href: '/' },\n+  { label: 'Docs', href: '/docs' },\n ];\n"}})}
	diagram := Thread{ID: "demo-diff", Name: "图表排版与导出设置", CWD: "/demo/diagram", HistoryMode: "legacy", UpdatedAt: 1793752200}
	diagram.Turns = []Turn{makeTurn("diagram-1", 1793752200, "调整图中文字的位置，说明同步与备份的区别，并更新导出配置。", []FileChange{
		{Path: "docs/overview.svg", Kind: ChangeKind{Type: "update"}, Diff: `@@ -148,11 +148,11 @@
   <g class="diagram-labels">
-    <text class="title" x="160" y="104">同步副本</text>
-    <text class="note" x="160" y="134">实时更新</text>
-    <text class="note" x="160" y="158">可以直接查询</text>
-    <text class="note" x="160" y="182">保留最新状态</text>
+    <text class="title" x="160" y="112">同步副本</text>
+    <text class="note" x="160" y="142">实时更新</text>
+    <text class="note" x="160" y="166">需要单独开启查询</text>
+    <text class="note" x="160" y="190">保留最新状态</text>
   <!-- Backup schedule -->
-    <text class="title" x="520" y="104">定期备份</text>
-    <text class="note" x="520" y="134">每天生成快照</text>
-    <text class="note" x="520" y="158">保存最近 7 个版本</text>
-    <text class="note" x="520" y="182">恢复时覆盖现有文件</text>
+    <text class="title" x="520" y="112">定期备份</text>
+    <text class="note" x="520" y="142">每天生成快照</text>
+    <text class="note" x="520" y="166">保存最近 30 个版本</text>
+    <text class="note" x="520" y="190">恢复时写入新目录</text>
   </g>
`},
		{Path: "src/export.ts", Kind: ChangeKind{Type: "update"}, Diff: `@@ -12,6 +12,7 @@
 export function exportDiagram(name: string) {
-  const format = 'png';
-  const scale = 1;
+  const format = 'svg';
+  const scale = 2;
+  const preserveText = true;
   return render(name, {
-    format, scale,
+    format, scale, preserveText,
   });
`},
		{Path: "export/settings.json", Kind: ChangeKind{Type: "update"}, Diff: `@@ -1,5 +1,5 @@
 {
-  "format": "png",
-  "scale": 1,
-  "directory": "output"
+  "format": "svg",
+  "scale": 2,
+  "directory": "exports"
 }
`},
	})}
	return &Demo{threads: []Thread{camera, website, diagram, child}}
}

func (d *Demo) ListThreads(_ context.Context, options ListOptions) ([]Thread, error) {
	var result []Thread
	if options.Archived {
		return result, nil
	}
	for _, t := range d.threads {
		if options.AncestorID == "" && t.ParentID == "" || options.AncestorID != "" && t.ParentID == options.AncestorID {
			t.Turns = nil
			result = append(result, t)
		}
	}
	return result, nil
}

func (d *Demo) ReadThread(_ context.Context, id string, full bool) (Thread, error) {
	for _, t := range d.threads {
		if t.ID == id {
			if !full {
				t.Turns = nil
			}
			return t, nil
		}
	}
	return Thread{}, fmt.Errorf("找不到演示会话 %s", id)
}

func (d *Demo) ReadTurns(ctx context.Context, id, _ string) (TurnPage, error) {
	t, err := d.ReadThread(ctx, id, true)
	return TurnPage{Data: t.Turns}, err
}
