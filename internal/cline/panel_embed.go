package cline

import _ "embed"

// PanelNavItem 管理面板侧栏本平台导航项（P5 面板拆分：片段为本包
// embed，panel/html.go 按原序拼装，产物与拆分前 AdminHTML 逐字节一致）。
//
//go:embed panel_nav.html
var PanelNavItem string

// PanelTabHTML 管理面板本平台 tab 片段（embed，同上拼装契约）。
//
//go:embed panel_tab.html
var PanelTabHTML string
