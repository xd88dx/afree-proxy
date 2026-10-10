package app

const adminHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AFree Proxy Admin</title>
<style>
:root{
  --bg:#0d0f12;--bg2:#131519;--bg3:#1a1d23;--panel:rgba(255,255,255,.028);
  --border:rgba(255,255,255,.08);--border-strong:rgba(255,255,255,.16);
  --text:#d6dae2;--text2:#8b919d;--text3:#5d626c;
  --accent:#8ab4f8;--accent2:#7ee2a8;--amber:#d9b04a;--danger:#e0705f;--pink:#f2a0b8;
  --accent-grad:linear-gradient(135deg,#8ab4f8,#a8c7fa);
  --glow:0 0 0 1px rgba(138,180,248,.2);
  --status-active-bg:rgba(126,226,168,.1);--status-cooldown-bg:rgba(217,176,74,.1);
  --status-expired-bg:rgba(224,112,95,.1);
  --btn-primary-bg:#2b4a77;--btn-primary-hover:#34578c;
  --btn-success-bg:#27513c;--btn-success-hover:#2f6249;
  --radius:10px;--radius-sm:7px;
}
[data-theme="light"]{
  --bg:#f6f7f8;--bg2:#ffffff;--bg3:#eef0f2;--panel:#ffffff;
  --border:rgba(15,18,22,.1);--border-strong:rgba(15,18,22,.22);
  --text:#1c1f24;--text2:#5c636e;--text3:#9aa0aa;
  --accent:#33629c;--accent2:#2b7a4b;--amber:#96700f;--danger:#b3442f;--pink:#b95c78;
  --accent-grad:linear-gradient(135deg,#33629c,#4a7ab5);
  --glow:0 0 0 1px rgba(51,98,156,.18);
  --status-active-bg:rgba(43,122,75,.1);--status-cooldown-bg:rgba(150,112,15,.1);
  --status-expired-bg:rgba(179,68,47,.08);
  --btn-primary-bg:#33629c;--btn-primary-hover:#2a5284;
  --btn-success-bg:#2b7a4b;--btn-success-hover:#246a40;
}
*{margin:0;padding:0;box-sizing:border-box}
/* color-scheme 让原生控件（select 弹出层/滚动条/复选框）跟随主题：
   深色主题下 select 弹层才会用深底浅字，否则浏览器默认浅底配白色文字几乎不可读 */
:root,[data-theme="dark"]{color-scheme:dark}
[data-theme="light"]{color-scheme:light}
select option{background:var(--bg2);color:var(--text)}
html{-webkit-text-size-adjust:100%}
body{font-family:'Inter','Segoe UI','PingFang SC','Microsoft YaHei',system-ui,sans-serif;background:var(--bg);color:var(--text);font-size:14px;line-height:1.55;min-height:100vh}
.mono,code{font-family:'JetBrains Mono','Cascadia Code','Fira Code',Consolas,monospace;font-size:12px}

/* ===== Layout ===== */
.layout{display:flex;min-height:100vh}
.sidebar{width:236px;background:var(--panel);backdrop-filter:blur(14px);border-right:1px solid var(--border);padding:18px 10px;flex-shrink:0;display:flex;flex-direction:column;position:sticky;top:0;height:100vh;overflow-y:auto}
.sidebar h1{font-size:15px;font-weight:700;padding:2px 10px 16px;border-bottom:1px solid var(--border);margin-bottom:10px;display:flex;align-items:center;gap:8px;letter-spacing:.02em}
.sidebar h1 .logo{width:28px;height:28px;border-radius:7px;background:var(--btn-primary-bg);display:inline-flex;align-items:center;justify-content:center;color:#fff;box-shadow:var(--glow)}
.sidebar h1 .logo svg{width:15px;height:15px}
.sidebar h1 .brand-name{color:var(--text)}
.sidebar h1 .theme-toggle{margin-left:auto;padding:4px 8px}
.sidebar h1 span{color:var(--accent)}
.nav-item{display:flex;align-items:center;gap:10px;padding:8px 12px;border-radius:8px;cursor:pointer;color:var(--text2);transition:.15s;font-size:13.5px;margin-bottom:1px;position:relative}
.nav-item .nav-ico{width:18px;height:18px;display:inline-flex;align-items:center;justify-content:center;flex-shrink:0}
.nav-item .nav-ico svg{width:16px;height:16px;stroke:currentColor;fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.nav-item:hover{color:var(--text);background:rgba(255,255,255,.05)}
.nav-item.active{color:var(--text);background:rgba(255,255,255,.07);font-weight:600}
.nav-item.active::before{content:'';position:absolute;left:-10px;top:22%;bottom:22%;width:2px;border-radius:2px;background:var(--accent)}
.nav-item.active .nav-ico svg{stroke:var(--accent)}
.nav-group{font-size:10.5px;font-weight:600;letter-spacing:.12em;text-transform:uppercase;color:var(--text3);padding:14px 12px 5px;user-select:none}
.sidebar-footer{margin-top:auto;padding:12px 8px 4px;font-size:11.5px;color:var(--text3);border-top:1px solid var(--border)}
.sidebar-footer a{color:var(--accent);text-decoration:none}
.main{flex:1;padding:26px 34px 60px;min-width:0;max-width:1500px;margin:0 auto;width:100%}
h2{font-size:21px;margin-bottom:18px;font-weight:700;letter-spacing:.01em}

/* ===== Dashboard platform cards ===== */
.dash-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:14px}
.dash-card{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);padding:16px 18px;cursor:pointer;transition:border-color .2s,transform .2s}
.dash-card:hover{border-color:var(--accent);transform:translateY(-1px)}
.dash-name{font-size:13px;font-weight:600;color:var(--text2)}
.dash-stats{font-size:14px;color:var(--text);margin-top:8px;font-variant-numeric:tabular-nums}
.stat-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:14px}
.stat-card{background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:var(--radius);padding:13px 16px}
.stat-name{font-size:12px;font-weight:600;color:var(--text2)}
.stat-line{font-size:13px;color:var(--text);margin-top:6px;font-variant-numeric:tabular-nums}
@keyframes rise{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:none}}

/* ===== Sections ===== */
.section{background:var(--panel);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:22px;overflow:hidden;animation:rise .4s ease both}
.section-title{padding:13px 18px;border-bottom:1px solid var(--border);font-weight:600;font-size:14px;display:flex;align-items:center;gap:8px}
.section-title .sec-ico{width:17px;height:17px;display:inline-flex;align-items:center;justify-content:center;flex-shrink:0}
.section-title .sec-ico svg{width:15px;height:15px;stroke:var(--text2);fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.section-body{padding:18px}

/* ===== Tables ===== */
.table-wrap{overflow-x:auto}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:10px 14px;border-bottom:1px solid var(--border);font-size:13px;white-space:nowrap}
th{color:var(--text3);font-weight:600;font-size:11.5px;text-transform:uppercase;letter-spacing:.06em}
tbody tr{transition:.12s}
tbody tr:hover{background:rgba(255,255,255,.035)}
tbody tr:last-child td{border-bottom:none}

/* ===== Status badges ===== */
.status{display:inline-flex;align-items:center;gap:6px;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:600}
.status.active{background:var(--status-active-bg);color:var(--accent2)}
.status.cooldown{background:var(--status-cooldown-bg);color:var(--amber)}
.status.expired{background:var(--status-expired-bg);color:var(--danger)}
.status-dot{width:7px;height:7px;border-radius:50%;display:inline-block}
.status-dot.active{background:var(--accent2)}
.status-dot.cooldown{background:var(--amber)}
.status-dot.expired{background:var(--danger)}

/* ===== Buttons ===== */
.btn{display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:7px 15px;border:1px solid var(--border);border-radius:var(--radius-sm);background:rgba(255,255,255,.04);color:var(--text);cursor:pointer;font-size:13px;transition:.15s;text-decoration:none;font-family:inherit;white-space:nowrap}
.btn:hover{background:rgba(255,255,255,.09);border-color:var(--border-strong)}
.btn:active{transform:none}
.btn svg{width:14px;height:14px;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0}
.btn-primary{background:var(--btn-primary-bg);border-color:transparent;color:#fff;font-weight:600}
.btn-primary:hover{background:var(--btn-primary-hover)}
.btn-success{background:var(--btn-success-bg);border-color:transparent;color:#fff;font-weight:600}
.btn-success:hover{background:var(--btn-success-hover)}
.btn-danger{border-color:rgba(224,112,95,.4);color:var(--danger);background:transparent}
.btn-danger:hover{background:rgba(224,112,95,.1);border-color:var(--danger)}
.btn-sm{padding:3px 10px;font-size:12px;border-radius:6px}
.btn-sm svg{width:13px;height:13px}

/* ===== Forms ===== */
input,textarea,select{width:100%;padding:9px 13px;background:rgba(0,0,0,.25);border:1px solid var(--border);border-radius:var(--radius-sm);color:var(--text);font-size:13px;font-family:inherit;transition:.15s}
[data-theme="light"] input,[data-theme="light"] textarea,[data-theme="light"] select{background:#fff}
input::placeholder,textarea::placeholder{color:var(--text3)}
input:focus,textarea:focus,select:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px rgba(138,180,248,.12)}
textarea{resize:vertical;min-height:84px;font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px}
select{cursor:pointer;appearance:none;background-image:linear-gradient(45deg,transparent 50%,var(--text2) 50%),linear-gradient(135deg,var(--text2) 50%,transparent 50%);background-position:calc(100% - 18px) 55%,calc(100% - 13px) 55%;background-size:5px 5px;background-repeat:no-repeat;padding-right:32px}
.form-row{display:flex;gap:14px;align-items:flex-end;margin-bottom:14px;flex-wrap:wrap}
/* textarea 默认 inline 布局的基线空隙会让它在 flex-end 行里比旁边的
   select 高出几像素 —— 改 block 贴齐字段底边 */
.field textarea{display:block}
.form-row .field{flex:1;min-width:180px}
.form-row .field label{display:block;font-size:12px;color:var(--text2);margin-bottom:6px;font-weight:500}
.form-actions{display:flex;gap:10px;margin-top:14px;flex-wrap:wrap}
/* 出口绑定联想下拉（combobox） */
.eg-input{width:170px;font-size:12px;padding:5px 8px;border-radius:7px;border:1px solid var(--border-strong);background:var(--bg3);color:var(--text);cursor:pointer}
.eg-input:focus{outline:none;border-color:var(--accent)}
.eg-menu{position:absolute;z-index:1200;background:var(--bg2);border:1px solid var(--border-strong);border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.35);max-height:260px;overflow-y:auto;min-width:220px}
.eg-item{padding:6px 10px;font-size:12px;cursor:pointer;color:var(--text);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.eg-item:hover{background:rgba(138,180,248,.15)}
.eg-item.active{background:rgba(138,180,248,.22)}
.eg-rate{float:right;margin-left:12px;color:var(--text3);font-variant-numeric:tabular-nums}
.eg-item.eg-hint{cursor:default;color:var(--text3)}
.eg-item.eg-hint:hover{background:none}
.flex{display:flex;align-items:center;gap:8px}
.text-right{text-align:right}
.inline-flex{display:inline-flex;align-items:center;gap:6px}
.justify-between{display:flex;justify-content:space-between;align-items:center;gap:10px;flex-wrap:wrap}
.hint{font-size:12px;color:var(--text2);margin-top:8px;line-height:1.6}
.hint strong{color:var(--text)}

/* ===== Dashboard: endpoints ===== */
.endpoint-list{display:flex;flex-direction:column;gap:8px}
.endpoint{display:flex;align-items:center;gap:12px;padding:9px 13px;background:rgba(255,255,255,.025);border:1px solid var(--border);border-radius:8px;flex-wrap:wrap}
.endpoint code{background:rgba(138,180,248,.08);border:1px solid rgba(138,180,248,.2);color:var(--accent);padding:3px 10px;border-radius:6px;font-size:11.5px;white-space:nowrap}
.endpoint span{font-size:12px;color:var(--text2);flex:1;min-width:160px}
.danger-zone{border-color:rgba(224,112,95,.28)}
.danger-zone .section-title{color:var(--danger)}
.auto-pill{font-size:11px;color:var(--text2);background:rgba(255,255,255,.05);border:1px solid var(--border);border-radius:999px;padding:2px 10px;white-space:nowrap}
.auto-pill.paused{color:var(--text3);border-color:var(--border)}

/* ===== Toast ===== */
/* 通知条：顶部居中悬浮，不顶格贴边，与页头留出呼吸空隙 */
.toast{position:fixed;top:18px;left:50%;padding:12px 22px;border-radius:12px;color:#fff;z-index:9999;opacity:0;transform:translate(-50%,-14px) scale(.97);transition:.3s cubic-bezier(.2,.9,.3,1.2);font-size:13px;max-width:520px;backdrop-filter:blur(12px);border:1px solid rgba(255,255,255,.14);box-shadow:0 12px 40px rgba(2,6,23,.5);white-space:pre-line;text-align:center}
.toast.show{opacity:1;transform:translate(-50%,0)}
.toast.success{background:#2b5a44}.toast.error{background:#7a3a30}
.toast.info{background:#31465e}
.toast.warning{background:#6e5619}
/* 拖拽排序：拖拽中的行半透明 + 顶部细高亮，落点感知 */
tr.row-dragging{opacity:.45;box-shadow:inset 0 2px 0 var(--accent)}
tr[draggable="true"]{cursor:grab}
tr[draggable="true"]:active{cursor:grabbing}

/* ===== Misc ===== */
.loading{display:inline-block;width:14px;height:14px;border:2px solid var(--text3);border-top-color:var(--accent);border-radius:50%;animation:spin .7s linear infinite;vertical-align:-2px}
@keyframes spin{to{transform:rotate(360deg)}}
.empty{padding:30px;text-align:center;color:var(--text2)}
.empty-state{padding:44px 20px;text-align:center;color:var(--text2)}
.empty-state .icon{font-size:40px;margin-bottom:10px;display:block;opacity:.8}
.key-display{background:rgba(0,0,0,.3);padding:9px 13px;border-radius:var(--radius-sm);border:1px solid var(--border);font-family:'JetBrains Mono','Cascadia Code',Consolas,monospace;font-size:12px;word-break:break-all;cursor:pointer;transition:.15s}
.key-display:hover{border-color:var(--accent)}
.model-tag{display:inline-block;padding:2px 9px;border-radius:6px;font-size:11px;background:rgba(255,255,255,.06);color:var(--text2);margin:2px;letter-spacing:.02em}
.model-tag.free{border:1px solid rgba(126,226,168,.35);color:var(--accent2);background:rgba(126,226,168,.07)}
.model-tag.context-1m{border:1px solid rgba(242,160,184,.45);color:var(--pink);background:rgba(242,160,184,.12)}
.theme-toggle{display:inline-flex;align-items:center;gap:5px;padding:4px 9px;border:1px solid var(--border);border-radius:7px;background:rgba(255,255,255,.04);color:var(--text2);cursor:pointer;font-size:12px;transition:.15s;font-family:inherit}
.theme-toggle:hover{color:var(--text);background:rgba(255,255,255,.09)}
[data-theme="dark"] .theme-toggle .light-label{display:none}
body:not([data-theme="dark"]) .theme-toggle .dark-label{display:none}
.probe-pill{font-size:11px;color:var(--text3)}

/* ===== WorkBuddy integrated console ===== */
.wb-toolbar{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.wb-switch{display:inline-flex;align-items:center;gap:8px;min-height:34px;font-size:13px;color:var(--text);cursor:pointer}
.wb-switch input{width:auto;padding:0;accent-color:var(--accent)}
.wb-config-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:22px 28px}
.wb-config-group{min-width:0}
.wb-config-group h3{font-size:13px;font-weight:650;color:var(--text);margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid var(--border)}
.wb-field{display:grid;grid-template-columns:minmax(112px,.8fr) minmax(120px,1.2fr);gap:12px;align-items:center;margin:9px 0}
.wb-field label{font-size:12px;color:var(--text2)}
.wb-field input,.wb-field select{min-width:0}
.wb-account{display:flex;align-items:center;gap:7px;min-width:190px}
.wb-account-main{min-width:0}
.wb-account-name{font-weight:600;overflow:hidden;text-overflow:ellipsis}
.wb-account-id{font-size:11px;color:var(--text3);font-family:'JetBrains Mono',Consolas,monospace;overflow:hidden;text-overflow:ellipsis}
.wb-usage{font-family:'JetBrains Mono',Consolas,monospace;font-size:11.5px;color:var(--text2)}
.wb-action-group{display:flex;gap:6px;justify-content:flex-end;flex-wrap:wrap}
dialog{position:fixed;inset:0;margin:auto;width:min(720px,calc(100vw - 32px));max-height:calc(100vh - 48px);overflow:auto;padding:0;border:1px solid var(--border-strong);border-radius:8px;background:var(--bg2);color:var(--text);box-shadow:0 24px 70px rgba(0,0,0,.48)}
dialog.wb-wide{width:min(1080px,calc(100vw - 32px))}
dialog::backdrop{background:rgba(0,0,0,.62);backdrop-filter:blur(3px)}
.toast[popover]{position:fixed;inset:18px auto auto 50%;margin:0;border:1px solid rgba(255,255,255,.14);color:#fff;pointer-events:none}
.wb-dialog-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 18px;border-bottom:1px solid var(--border);font-weight:650}
.wb-dialog-body{padding:18px}
.wb-dialog-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:16px}
.wb-link-row{display:flex;gap:8px;margin-top:10px}
.wb-link-row input{min-width:0}
.wb-task-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.wb-pk-card{border:1px solid var(--border);border-radius:8px;padding:14px;background:rgba(255,255,255,.025);margin-bottom:16px}
.wb-pk-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;flex-wrap:wrap}
.wb-pk-name{font-weight:650;font-size:15px}
.wb-pk-big{font-size:26px;font-weight:700;font-variant-numeric:tabular-nums;margin-top:10px}
.wb-pk-sub{font-size:12px;color:var(--text2);margin-top:2px}
.wb-pk-mix{display:flex;height:8px;overflow:hidden;border-radius:999px;background:var(--bg3);margin-top:12px}
.wb-pk-mix i{display:block;min-width:2px}
.wb-pk-legend{display:flex;gap:8px 14px;flex-wrap:wrap;margin-top:10px;font-size:12px;color:var(--text2)}
.wb-pk-legend span{display:inline-flex;align-items:center;gap:6px}
.wb-pk-legend i{width:8px;height:8px;border-radius:2px;flex:none}
.wb-todo-group{border:1px solid var(--border);border-radius:8px;padding:12px;margin:10px 0;background:rgba(255,255,255,.02)}
.wb-todo-title{display:flex;align-items:center;gap:8px;font-weight:650;cursor:pointer;list-style:none}
.wb-todo-title::-webkit-details-marker{display:none}
.wb-todo-title::before{content:'▶';font-size:10px;color:var(--text3);transition:transform .15s}
.wb-todo-group[open] .wb-todo-title::before{transform:rotate(90deg)}
.wb-todo-title .auto-pill{margin-left:auto}
.wb-todo-row{display:flex;align-items:flex-start;gap:10px;padding:7px 0;border-top:1px solid var(--border)}
.wb-todo-row:first-of-type{border-top:0}
.wb-todo-row .nm{flex:1;min-width:0}
.wb-todo-row .id{font-size:11px;color:var(--text3);font-family:'JetBrains Mono',Consolas,monospace}

/* ===== Responsive ===== */
@media (max-width:980px){
  .layout{flex-direction:column}
  .sidebar{width:100%;height:auto;position:sticky;top:0;flex-direction:row;align-items:center;padding:10px 12px;overflow-x:auto;gap:4px;z-index:50}
  .sidebar h1{display:flex;align-items:center;border-bottom:none;margin:0;padding:0 6px 0 0;gap:6px;font-size:13px;white-space:nowrap}
  .sidebar h1 .brand-name{display:none}
  .sidebar .nav-item{padding:7px 11px;white-space:nowrap}
  .sidebar .nav-item.active::before{display:none}
  .nav-group{display:none}
  .sidebar .theme-toggle{margin-left:4px}
  .sidebar-footer{display:none}
  .main{padding:20px 16px 48px}
}
@media (max-width:560px){
  h2{font-size:18px}
  .section-title{padding:11px 14px}
  .section-body{padding:14px}
  .form-row .field{min-width:100%}
  .wb-config-grid{grid-template-columns:1fr}
  .wb-field{grid-template-columns:1fr;gap:5px}
  .wb-toolbar{width:100%;justify-content:flex-start}
}
/* 账号表暂存编辑：有未保存修改的行高亮，对应平台的"保存"按钮加描边提醒 */
.tr-dirty > td{background:rgba(234,179,8,.09)}
.dirty-hint{box-shadow:0 0 0 2px var(--accent)}
.btn-warn{background:var(--amber);border-color:var(--amber);color:#1a1a1a;font-weight:600}
.btn-warn:hover{filter:brightness(1.1);background:var(--amber);border-color:var(--amber)}
</style>
</head>
<body>
<div class="layout">
<div class="sidebar">
<h1><span class="logo"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/></svg></span><span class="brand-name">AFree Proxy</span><button class="theme-toggle" id="langBtn" onclick="toggleLang()" title="Switch language / 切换语言" style="margin-left:6px"></button><button class="theme-toggle" onclick="toggleTheme()" title="Toggle theme"><span class="icon" id="themeIcon"></span><span class="light-label">Light</span><span class="dark-label">Dark</span></button></h1>
<div class="nav-item active" data-tab="dashboard"><span class="nav-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/></svg></span> Dashboard</div>
<div class="nav-group">Account pool</div>
<div class="nav-item" data-tab="accounts"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4 21c0-4 3.6-6.5 8-6.5s8 2.5 8 6.5"/></svg></span> Cline</div>
<div class="nav-item" data-tab="opencode"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14.5 14.5 0 0 1 0 18 14.5 14.5 0 0 1 0-18z"/></svg></span> OpenCode</div>
<div class="nav-item" data-tab="workbuddy"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3z"/><path d="M12 12l8-4.5M12 12v9M12 12L4 7.5"/></svg></span> WorkBuddy</div>
<div class="nav-group">Services</div>
<div class="nav-item" data-tab="proxypool"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v4m0 0-5.5 5m5.5-5 5.5 5"/></svg></span> Proxy pool</div>
<div class="nav-item" data-tab="settings"><span class="nav-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34h0a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55h0a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v0a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.55 1z"/></svg></span> Gateway settings</div>
<div class="nav-item" data-tab="combos"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M8 3v5a4 4 0 0 1-4 4 4 4 0 0 1 4 4v5M16 3v5a4 4 0 0 0 4 4 4 4 0 0 0-4 4v5"/></svg></span> Custom aliases</div>
<div class="nav-item" data-tab="logs"><span class="nav-ico"><svg viewBox="0 0 24 24"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg></span> Request logs</div>
<div class="sidebar-footer">
  <div>Admin panel: <a href="/admin/">/admin/</a></div>
  <div>API address: <span id="footerApiAddr">http://127.0.0.1:3457</span></div>
</div>
</div>

<div class="main">

<div id="tab-dashboard" class="tab-panel">
<h2>Dashboard</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg></span> Platform overview</div>
  <div class="section-body">
    <div class="dash-grid">
      <div class="dash-card" onclick="switchTab('accounts')">
        <div class="dash-name">Cline</div>
        <div class="dash-stats" id="dashCline">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('opencode')">
        <div class="dash-name">OpenCode</div>
        <div class="dash-stats" id="dashOc">-</div>
      </div>
      <div class="dash-card" onclick="switchTab('workbuddy')">
        <div class="dash-name">WorkBuddy</div>
        <div class="dash-stats" id="dashWb">-</div>
      </div>
    </div>
    <div class="hint" style="margin:10px 0 0">Click a platform card to open its management page.</div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 20v-8m5 8V8m5 12v-5m5 5V4"/><path d="M3 21h18"/></svg></span> Request statistics</div>
  <div class="section-body">
    <div class="stat-grid">
      <div class="stat-card"><div class="stat-name">Cline</div><div class="stat-line" id="reqCline">-</div></div>
      <div class="stat-card"><div class="stat-name">OpenCode</div><div class="stat-line" id="reqZen">-</div></div>
      <div class="stat-card"><div class="stat-name">WorkBuddy</div><div class="stat-line" id="reqWb">-</div></div>
    </div>
    <div class="hint" style="margin:10px 0 0" id="reqStatsHint">Counted since process start.</div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M9 12h6M12 9v6"/><circle cx="12" cy="12" r="9"/></svg></span> Endpoints</div>
  <div class="section-body">
    <div class="endpoint-list">
      <div class="endpoint"><code>POST /v1/chat/completions</code><span>OpenAI Chat format — works with most tools out of the box</span></div>
      <div class="endpoint"><code>POST /v1/messages</code><span>Anthropic Messages format — for Claude Code / Cline and similar clients</span></div>
      <div class="endpoint"><code>POST /v1/responses</code><span>OpenAI Responses format — for Cursor and similar clients</span></div>
      <div class="endpoint"><code>GET /v1/models</code><span>Model list</span></div>
    </div>
  </div>
</div>
</div>

<div id="tab-accounts" class="tab-panel" style="display:none">
  <div class="flex justify-between" style="margin-bottom:16px">
  <div>
    <h2 style="margin-bottom:4px">Cline</h2>
    <div class="hint" id="accSummary" style="margin:0">Loading...</div>
  </div>
</div>
<div class="hint" style="margin:-4px 0 16px;padding:11px 14px;border:1px solid var(--border);border-radius:10px;background:rgba(148,163,184,.05)">
  <strong style="color:var(--text)">Tokens</strong> are counted locally by this proxy (input+output; exact when upstream returns usage, otherwise estimated from the request body) to gauge headroom before official rate limits. The Test button sends a real probe request. The Reset button <strong style="color:var(--text)">probes upstream rate-limit status</strong>: if still limited it stays in cooldown and shows the estimated recovery time; only a passing probe lifts the cooldown and resets today's stats.
  <strong style="color:var(--text)">Proxy binding</strong> (isolation mode, on by default): a bound account always egresses via its main proxy, falls back to the backup, and is <em>skipped entirely</em> while both are unavailable — never routed through another exit. Configure the list on the <a href="#" onclick="switchTab('proxypool');return false" style="color:var(--accent);cursor:pointer">Proxy pool</a> page.
</div>
<div class="section">
  <div class="section-body" style="padding:6px">
    <div class="table-wrap">
    <table>
      <thead>
        <tr><th>Email</th><th>Status</th><th title="Counted locally by this proxy — not the official free quota">Today/Total tokens</th><th>Last used</th><th>Created</th><th title="Main / backup egress — isolation mode picks main first, then backup; both down = account skipped">Proxy binding</th><th>Actions</th><th style="text-align:center" title="Routing participation. Newly added accounts start unchecked — tick Pool to join rotation.">Pool</th></tr>
      </thead>
      <tbody id="accountTableBody">
        <tr><td colspan="8" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openAccAddDialog()">Add</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllAccountsPool(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllAccountsPool(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageClearAccountProxies()" title="Stage: clear proxy bindings on ALL accounts AND disable them — click Save to apply">Clear bindings</button>
        <button class="btn btn-sm" onclick="applyProxiesToOthers('acc')" title="Copy this page's proxy bindings by row order to the other two account pools. Takes effect immediately; only proxy bindings are changed, enable/disable states are untouched.">Apply to others</button>
        <button class="btn btn-sm" id="accTestBtn" onclick="batchTestAccounts()" title="Run the Test probe on every account (same as clicking Test on each row)">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="accResetBtn" onclick="resetAccountEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="accSaveBtn" onclick="saveAccountEdits()">Save</button>
      </div>
    </div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 7h16M4 12h16M4 17h10"/></svg></span> Request headers (mimicking the Cline CLI)<button class="btn btn-sm" onclick="resetHeaders()" style="margin-left:auto" title="Restore the default Cline CLI headers">Restore defaults</button></div>
  <div class="section-body">
    <div class="table-wrap">
    <table>
      <thead><tr><th style="width:220px">Header</th><th>Value</th><th style="width:40px"></th></tr></thead>
      <tbody id="headersTableBody">
        <tr><td colspan="3" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
    <div class="form-actions">
      <button class="btn btn-sm" onclick="addHeaderRow()">Add header</button>
      <button class="btn btn-sm btn-primary" onclick="saveHeaders()">Save headers</button>
    </div>
    <div class="hint">These headers are attached to every request forwarded to the Cline API to mimic the official client.</div>
    <div id="headerSaveResult" style="margin-top:8px"></div>
  </div>
</div>
</div>

<div id="tab-proxypool" class="tab-panel" style="display:none">
<h2>Proxy pool</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v4m0 0-5.5 5m5.5-5 5.5 5"/></svg></span> Egress proxies</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:10px">One proxy list serves every upstream that opts in below. One proxy per line: <code>http://user:pass@host:port</code> or <code>socks5://host:port</code>. Requests rotate across healthy proxies per request; a proxy that hits a rate limit is cooled down and skipped automatically.</div>
    <div class="form-row">
      <div class="field" style="flex:3"><label>Proxy list</label>
        <textarea id="ppProxies" rows="4" placeholder="one per line: socks://b64(user:pass)@host:port#alias, socks5://user:pass@host:port#alias or http://host:port"></textarea>
      </div>
      <div class="field"><label>Proxy selection strategy</label>
        <select id="ppStrategy"><option value="round_robin">Round-robin (round_robin)</option><option value="random">Random (random)</option><option value="fill">Fill (fill)</option></select>
      </div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveProxyPool()">Save proxy pool</button></div>
    <div id="ppSaveResult" style="margin-top:8px"></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg></span> Global policy</div>
  <div class="section-body">
    <div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:10px 24px;align-items:center">
      <div class="field" style="margin:0"><label style="margin:0">Cline</label></div>
      <div class="field" style="margin:0"><label style="margin:0">OpenCode</label></div>
      <div class="field" style="margin:0"><label style="margin:0">WorkBuddy</label></div>
      <div class="field" style="margin:0">
        <select id="ppCline"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
      </div>
      <div class="field" style="margin:0">
        <select id="ppZen"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
      </div>
      <div class="field" style="margin:0">
        <select id="ppWb"><option value="true">Proxy</option><option value="false">Direct connection</option></select>
      </div>
      <div class="hint" style="margin:0">Cline accounts without proxy bindings follow this switch.</div>
      <div class="hint" style="margin:0">OpenCode keys without proxy bindings follow this switch.</div>
      <div class="hint" style="margin:0">WorkBuddy accounts without proxy bindings follow this switch.</div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveGlobalPolicy()">Save</button></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="3" y="11" width="18" height="10" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg></span> Proxy isolation (identity ↔ exit binding)</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Isolation mode</label>
        <div style="display:flex;gap:6px;align-items:center">
          <select id="ppIsolation" style="flex:1">
            <option value="true">Enabled (default) — bound identities only ever use their bound exits</option>
            <option value="false">Disabled — ignore bindings; exits rotate per the strategy above (direct when pool off)</option>
          </select>
          <label style="display:flex;gap:6px;align-items:center;white-space:nowrap;margin:0">
            <input type="checkbox" id="ppBackupEnabled" style="width:auto;padding:0;accent-color:var(--accent)">
            Enable backup proxies
          </label>
        </div>
      </div>
    </div>
    <div class="hint">With isolation on, every identity (account or key) only ever egresses through its bound exits — main proxy first, backup as fallback; when both are unavailable the identity is skipped, never falling back to another exit or a direct connection. The <code>PROXY_ISOLATION</code> env var only accepts true/false: true forces this on (read-only toggle); false just changes the default to off.</div>
    <div class="hint" id="ppIsolationHint" style="margin-top:4px"></div>
    <div class="hint" id="ppBackupHint" style="margin-top:4px">Off by default: every identity egresses through its main proxy only — when the main proxy is cooling down or removed, the identity is skipped for that round, never trying the backup and never falling back to a direct connection. When enabled, backups act as the fallback for the main proxy to keep identities available as much as possible.</div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveIsolationOptions()">Save</button></div>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 17l5-6 4 3 4-7 5 4"/><path d="M3 21h18"/></svg></span> Proxy health (online rate)</div>
  <div class="section-body">
    <div class="table-wrap">
    <table>
      <thead><tr>
        <th>Proxy</th>
        <th style="text-align:center" title="Upstream attempts that egressed via this proxy since the pool was last edited">Attempts</th>
        <th style="text-align:center" title="Transport-layer success rate since the pool was last edited (upstream 4xx/5xx still counts as the proxy being online)">Rate</th>
        <th style="text-align:center" title="Success rate over the most recent attempts (window: last 50). Below 80% with at least 10 samples is highlighted red — time to replace the proxy.">Recent</th>
        <th>Last OK</th>
        <th>Last Fail</th>
        <th>Cooldown</th>
      </tr></thead>
      <tbody id="ppHealthBody"><tr><td colspan="7" class="empty">Loading...</td></tr></tbody>
    </table>
    </div>
    <div class="hint" style="margin-top:8px">Rates are passively sampled from real upstream traffic only — no probing traffic is generated. "No data" means this exit has not been used since the pool was last edited (with the fill strategy only the first proxy gets traffic; global switches and per-identity bindings decide the rest). Editing the proxy list resets all counters.</div>
  </div>
</div>
</div>

<div id="tab-settings" class="tab-panel" style="display:none">
<h2>Gateway settings</h2>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span> API keys</div>
  <div class="section-body">
    <p class="hint">Generated keys authenticate client access to the proxy API; they only take effect when the API_KEY environment variable is not set.</p>
    <div class="form-actions" style="margin-bottom:14px">
      <button class="btn btn-success" onclick="generateKey()">Generate new key</button>
    </div>
    <div id="keysList"></div>
    <div id="keyGenResult" style="margin-top:8px"></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><rect x="2" y="7" width="20" height="14" rx="2"/><path d="M16 7V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v2"/><path d="M12 12v3M8.5 13.5v0M15.5 13.5v0"/></svg></span> Available models <span id="modelsProbeInfo" class="probe-pill" style="font-weight:normal"></span></div>
  <div class="section-body">
    <div class="flex" style="margin-bottom:10px;gap:10px">
      <button class="btn btn-sm btn-primary" onclick="refreshModels()">Refresh models</button>
      <span class="hint" style="margin:0">Auto-syncs the official model feeds from upstreams (60s)</span>
    </div>
    <div id="modelsList">Loading...</div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M12 2v4m0 12v4M2 12h4m12 0h4M4.9 4.9l2.8 2.8m8.6 8.6 2.8 2.8m0-14.2-2.8 2.8M7.7 16.3l-2.8 2.8"/></svg></span> General config</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Listen address</label><input type="text" id="settingAddr" disabled></div>
      <div class="field">
        <label>Account scheduling strategy</label>
        <select id="settingStrategy" onchange="updateConfig()">
          <option value="round_robin">Round-robin (round_robin)</option>
          <option value="random">Random (random)</option>
          <option value="fill">Fill (fill)</option>
        </select>
      </div>
    </div>
    <div class="form-row" style="align-items:flex-start">
      <div class="field">
        <label>Platform</label>
        <select id="settingModelPlatform" onchange="loadModelOptions()">
          <option value="cline" selected>Cline</option>
          <option value="zen">OpenCode</option>
          <option value="workbuddy">WorkBuddy</option>
        </select>
        <div style="margin-top:18px"><button class="btn btn-sm btn-primary" onclick="saveDefaultModel()">Save</button></div>
      </div>
      <div class="field">
        <label>Default model</label>
        <select id="settingDefModel" style="font-family:'JetBrains Mono',Consolas,monospace"></select>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 3v12m0 0 4-4m-4 4-4-4"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg></span> Config export / import</div>
  <div class="section-body">
    <div class="hint">Import/export all persisted configuration. The export file contains private information; keep it safe. Import is an overwrite operation, so proceed with caution.</div>
    <div class="form-actions">
      <button class="btn btn-success" onclick="exportConfig()">Export config</button>
      <button class="btn btn-primary" onclick="document.getElementById('importFile').click()">Import config</button>
      <input type="file" id="importFile" accept=".json,application/json" style="display:none" onchange="onImportFileChange(event)">
    </div>
  </div>
</div>

<div class="section danger-zone">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/></svg></span> Danger zone</div>
  <div class="section-body">
    <div class="hint" style="margin-top:0">Deletes all persisted data, including but not limited to accounts and configuration. This cannot be undone; proceed with caution!</div>
    <div class="form-actions">
      <button class="btn btn-danger" onclick="deleteAllData()">Delete all data</button>
    </div>
  </div>
</div>
</div>

<div id="tab-logs" class="tab-panel" style="display:none">
<div class="flex justify-between" style="margin-bottom:16px">
  <h2>Request logs <span class="probe-pill" style="font-weight:normal">last 500 entries, persisted to data/requests.jsonl</span></h2>
  <div style="display:flex;gap:8px;align-items:center">
    <label style="display:flex;gap:6px;align-items:center;white-space:nowrap;margin:0">
      <input type="checkbox" id="settingAdminLog" onchange="saveAdminLogToggle()" style="width:auto;padding:0;accent-color:var(--accent)">
      Record admin logs
    </label>
    <span class="auto-pill" id="logsAutoPill">Auto-refreshing</span>
    <button class="btn btn-sm" onclick="toggleLogsAuto()" id="logsAutoBtn">Pause</button>
    <button class="btn btn-sm" onclick="loadLogs()">Refresh</button>
  </div>
</div>
<div class="section">
  <div class="section-body" style="padding:6px">
    <div class="table-wrap">
    <table>
      <thead>
        <tr><th>Time</th><th>Source</th><th>Method</th><th>Path</th><th>Model</th><th>Route</th><th>Upstream</th><th>Proxy</th><th>Status</th><th>Duration</th></tr>
      </thead>
      <tbody id="logsTableBody">
        <tr><td colspan="10" class="empty">Loading...</td></tr>
      </tbody>
    </table>
    </div>
  </div>
</div>
</div>

<div id="tab-combos" class="tab-panel" style="display:none">
<h2>Custom aliases</h2>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg></span> Create custom alias</div>
  <div class="section-body">
    <div class="hint" style="margin-bottom:10px">Clients request the alias ID as their model, and the proxy rewrites it to the target model for the chosen platform. Alias IDs are user-defined (e.g. <code>cline-glm-5.3</code>) and must not collide with real model IDs; the target must belong to the same platform — cross-platform targets are not allowed.</div>
    <div class="form-row">
      <div class="field"><label>Alias ID</label><input type="text" id="comboId" placeholder="cline-glm-5.3"></div>
      <div class="field"><label>Platform</label>
        <select id="comboPlatform" onchange="fillComboModels()"><option value="cline">cline</option><option value="zen">opencode</option><option value="workbuddy">workbuddy</option></select>
      </div>
      <div class="field" style="flex:2"><label>Target model</label><select id="comboTarget"></select></div>
      <div class="field" style="flex:0 0 auto;display:flex;align-items:flex-end"><label style="display:flex;gap:6px;align-items:center;white-space:nowrap;padding-bottom:8px"><input type="checkbox" id="comboUseProxies"> Use proxy pool</label></div>
    </div>
    <button class="btn btn-primary" onclick="createCombo()">Create custom alias</button>
  </div>
</div>
<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg></span> Existing custom aliases</div>
  <div class="section-body" id="combosList">Loading...</div>
</div>
</div>

<div id="tab-opencode" class="tab-panel" style="display:none">
<h2 style="margin-bottom:4px">OpenCode</h2>
<div class="hint" id="ocSummary" style="margin:0 0 16px">Loading...</div>

<div class="section">
  <div class="section-body">
    <div class="form-row">
      <div class="field" style="margin:0">
        <label>Base URL</label>
        <div style="display:flex;gap:8px;align-items:center">
          <input type="text" id="ocBaseURL" placeholder="https://opencode.ai/zen/v1" style="flex:1;min-width:0">
          <button class="btn btn-primary btn-sm" onclick="saveOcConfig()">Confirm</button>
        </div>
      </div>
    </div>
    <div class="flex" style="gap:10px;margin:0 0 8px;align-items:center;flex-wrap:wrap">
      <label class="hint" style="margin:0">Probe model (used by the Test buttons):</label>
      <select id="ocProbeModel" style="max-width:360px"><option value="">auto — big-pickle first, then live models</option></select>
    </div>
    <div class="table-wrap" style="margin-bottom:10px">
      <table>
        <thead><tr><th style="width:50px">#</th><th style="width:110px">Key</th><th style="width:70px">Usage</th><th style="width:110px">Session</th><th>Cooldown</th><th title="Main / backup egress — isolation mode picks main first, then backup; both down = key skipped. The harvester also mints through the bound exit.">Proxy binding</th><th style="width:130px"></th><th style="text-align:center" title="Routing participation: unchecked keys never enter request rotation, and minting follows routing (a routing-disabled key is no longer auto-minted). Newly added keys start disabled — tick Pool to enable.">Pool</th></tr></thead>
        <tbody id="ocKeysBody"><tr><td colspan="8" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:10px 6px 4px">
      <button class="btn btn-primary btn-sm" onclick="openZenKeyAddDialog()">Add credentials</button>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm" onclick="stageAllKeysRouting(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllKeysRouting(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageClearZenKeyProxies()" title="Stage: clear proxy bindings on ALL keys and disable routing — click Save to apply">Clear bindings</button>
        <button class="btn btn-sm" onclick="applyProxiesToOthers('key')" title="Copy this page's proxy bindings by row order to the other two account pools. Takes effect immediately; only proxy bindings are changed, enable/disable states are untouched.">Apply to others</button>
        <button class="btn btn-sm" id="ocTestBtn" onclick="batchTestKeys()" title="Run the Test probe on every key (same as clicking Test on each row)">Test all</button>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-sm btn-warn" id="ocResetBtn" onclick="resetKeyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="ocKeysSaveBtn" onclick="saveKeyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Rate-limit defense</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Max concurrency</label><input type="text" id="ocMaxConc" placeholder="8"></div>
      <div class="field"><label>Rate-limit retries</label><input type="text" id="ocRetries" placeholder="3"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOcConfig()">Save rate-limit config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M4 8V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2M4 8v10a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8M4 8h16"/><path d="M10 12h4"/></svg></span> Context compaction (OpenCode official mechanism)</div>
  <div class="section-body">
    <div class="form-row">
      <div class="field"><label>Auto-compaction</label>
        <select id="ocCompactAuto"><option value="true">On</option><option value="false">Off</option></select>
      </div>
      <div class="field"><label>Reserved buffer</label><input type="text" id="ocCompactBuffer" placeholder="20000"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Tail tokens kept</label><input type="text" id="ocKeepTokens" placeholder="8000"></div>
      <div class="field"><label>Summary model</label><input type="text" id="ocSummaryModel" placeholder="empty = same as request model"></div>
    </div>
    <div class="form-row">
      <div class="field"><label>Summary cap</label><input type="text" id="ocMaxSummary" placeholder="4096"></div>
    </div>
    <div class="form-actions"><button class="btn btn-primary" onclick="saveOcConfig()">Save compaction config</button></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg></span> Live session IDs (OpenCode FreeTier gate)
    <span id="ocSessSummary" class="probe-pill" style="font-weight:normal;margin-left:auto"></span>
  </div>
  <div class="section-body">
    <p class="hint" style="margin-top:0" id="ocSessHint">The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session <b>always</b> fails with 403 — normally the harvester mints one on startup, on repeated 403s, and every few hours; use the buttons below to mint immediately (e.g. right after a fresh deploy with many keys).</p>
    <div class="form-actions" style="margin-bottom:10px;align-items:center">
      <button class="btn btn-primary" id="ocSessBtnMissing" onclick="mintZenSessions(false)">Mint missing sessions</button>
      <button class="btn" id="ocSessBtnForce" onclick="mintZenSessions(true)">Force mint / refresh all</button>
      <span class="hint" style="margin:0" id="ocSessTimer"></span>
    </div>
    <div class="table-wrap">
      <table>
        <thead><tr><th style="width:70px">Key</th><th style="width:130px">Session</th><th style="width:150px">State</th><th>Last minted</th></tr></thead>
        <tbody id="ocSessBody"><tr><td colspan="4" class="empty">Loading...</td></tr></tbody>
      </table>
    </div>
    <div id="ocSessResult" style="margin-top:10px"></div>
  </div>
</div>

<div class="section">
  <div class="section-title"><span class="sec-ico"><svg viewBox="0 0 24 24"><path d="M3 3v18h18"/><path d="M7 15l4-4 3 3 5-6"/></svg></span> OpenCode stats</div>
  <div class="section-body">
    <div class="table-wrap"><div id="ocStatsBox"></div></div>
    <div id="ocModelStatsBox" style="margin-top:14px"></div>
  </div>
</div>
</div>

<div id="tab-workbuddy" class="tab-panel" style="display:none">
  <div class="flex justify-between" style="margin-bottom:16px">
  <div>
    <h2 style="margin-bottom:4px">WorkBuddy</h2>
    <div class="hint" id="wbSummary" style="margin:0">Loading...</div>
  </div>
  <a class="btn btn-sm" href="/admin/workbuddy/" target="_blank" rel="noopener" style="align-self:flex-start">Original panel</a>
</div>

<div class="section">
  <div class="section-body" style="padding:6px">
    <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin:4px 6px 10px">
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="openWorkbuddyTodoScan()">Task center</button>
        <button class="btn btn-sm" onclick="refreshWorkbuddyAccounts()" title="Refresh all account balances">Refresh all</button>
      </span>
      <span class="wb-toolbar">
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('checkin_all')">Check in all</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('travel_all')">Pet patrol</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('activity_all')">Activity all</button>
        <button class="btn btn-sm" onclick="runWorkbuddyBatch('keepalive_all')">Refresh login</button>
      </span>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Account</th>
            <th>Status</th>
            <th>Credits</th>
            <th>Usage</th>
            <th>Success / errors</th>
            <th>Last success</th>
            <th title="Main / backup egress — isolation mode picks main first, then backup; both down = account skipped">Proxy binding</th>
            <th>Actions</th>
            <th style="text-align:center" title="Participates in rotation. Newly added accounts start disabled — tick Pool to join rotation.">Pool</th>
          </tr>
        </thead>
        <tbody id="wbAccountsBody">
          <tr><td colspan="9" class="empty">Loading...</td></tr>
        </tbody>
      </table>
    </div>
    <div style="display:flex;align-items:center;gap:8px;margin:10px 6px 4px">
      <div style="flex:1;display:flex;gap:8px;justify-content:flex-start">
        <button class="btn btn-primary btn-sm" onclick="openWorkbuddyAdd()">Add account</button>
      </div>
      <div style="flex:1;display:flex;gap:8px;justify-content:center">
        <button class="btn btn-sm" onclick="stageAllWbPool(true)">Enable all</button>
        <button class="btn btn-sm" onclick="stageAllWbPool(false)">Disable all</button>
        <button class="btn btn-sm" onclick="stageClearWorkbuddyProxyBindings()" title="Stage: clear proxy bindings on ALL WorkBuddy accounts and disable them — click Save to apply">Clear bindings</button>
        <button class="btn btn-sm" onclick="applyProxiesToOthers('wb')" title="Copy this page's proxy bindings by row order to the other two account pools. Takes effect immediately; only proxy bindings are changed, enable/disable states are untouched.">Apply to others</button>
      </div>
      <div style="flex:1;display:flex;gap:8px;justify-content:flex-end">
        <button class="btn btn-sm btn-warn" id="wbResetBtn" onclick="resetWorkbuddyEdits()" title="Discard staged changes and reload the last saved state">Discard</button>
        <button class="btn btn-primary btn-sm" id="wbSaveBtn" onclick="saveWorkbuddyEdits()">Save</button>
      </div>
    </div>
  </div>
</div>

<form id="wbConfigForm">
  <div class="section">
    <div class="section-title">
      <span class="sec-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34h0a1.7 1.7 0 0 0 1-1.55V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55h0a1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v0a1.7 1.7 0 0 0 1.55 1H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.55 1z"/></svg></span>
      WorkBuddy configuration
      <span style="flex:1"></span>
      <span class="auto-pill" id="wbConfigNote">Managed by the admin session</span>
    </div>
    <div class="section-body">
      <div class="hint" style="margin-top:0;margin-bottom:16px">Listening, credentials, and data paths stay under the gateway admin session.</div>
      <div class="wb-config-grid">
        <div class="wb-config-group">
          <h3>Scheduled jobs</h3>
          <div class="wb-switch"><input type="checkbox" id="wbCheckinEnabled" data-wb-path="schedule.checkin_enabled"><label for="wbCheckinEnabled">Automatic check-in</label></div>
          <div class="wb-field"><label for="wbCheckinHours">Check-in hours</label><input id="wbCheckinHours" data-wb-path="schedule.checkin_hours" data-wb-type="ints" placeholder="9, 21"></div>
          <div class="wb-switch"><input type="checkbox" id="wbTravelEnabled" data-wb-path="schedule.travel_enabled"><label for="wbTravelEnabled">Automatic patrol</label></div>
          <div class="wb-field"><label for="wbTravelHours">Patrol hours</label><input id="wbTravelHours" data-wb-path="schedule.travel_hours" data-wb-type="ints" placeholder="9, 21"></div>
          <div class="wb-switch"><input type="checkbox" id="wbActivityEnabled" data-wb-path="schedule.activity_enabled"><label for="wbActivityEnabled">Automatic activity</label></div>
          <div class="wb-field"><label for="wbActivityHours">Activity hours</label><input id="wbActivityHours" data-wb-path="schedule.activity_hours" data-wb-type="ints" placeholder="10"></div>
          <div class="wb-switch"><input type="checkbox" id="wbKeepaliveEnabled" data-wb-path="schedule.keepalive_enabled"><label for="wbKeepaliveEnabled">Automatic keepalive</label></div>
          <div class="wb-field"><label for="wbKeepaliveHours">Keepalive hours</label><input id="wbKeepaliveHours" data-wb-path="schedule.keepalive_hours" data-wb-type="ints" placeholder="22"></div>
          <div class="wb-switch"><input type="checkbox" id="wbBalanceEnabled" data-wb-path="schedule.balance_refresh_enabled"><label for="wbBalanceEnabled">Background balance refresh</label></div>
          <div class="wb-field"><label for="wbBalanceMinutes">Refresh interval (min)</label><input id="wbBalanceMinutes" data-wb-path="schedule.balance_refresh_minutes" data-wb-type="number" min="1" placeholder="5"></div>
          <div class="wb-switch"><input type="checkbox" id="wbGrowthEnabled" data-wb-path="schedule.growth_enabled"><label for="wbGrowthEnabled">Growth tasks</label></div>
          <div class="wb-field"><label for="wbGrowthHours">Growth hours</label><input id="wbGrowthHours" data-wb-path="schedule.growth_hours" data-wb-type="ints" placeholder="1"></div>
        </div>

        <div class="wb-config-group">
          <h3>Pool and cooldown</h3>
          <div class="wb-field"><label for="wbMaxInFlight">Max in-flight / account</label><input id="wbMaxInFlight" data-wb-path="pool.max_in_flight" data-wb-type="number" min="0" placeholder="3"></div>
          <div class="wb-field"><label for="wbModelRateFilter">Model rate filter</label>
            <select id="wbModelRateFilter" data-wb-path="pool.model_rate_filter" data-wb-type="number">
              <option value="0">0 — free only</option>
              <option value="0.1">0.1</option>
              <option value="0.2">0.2</option>
              <option value="0.3">0.3</option>
              <option value="0.5">0.5</option>
              <option value="1">1</option>
            </select>
          </div>
          <div class="wb-field"><label for="wbMaxGlobal">Global in-flight limit</label><input id="wbMaxGlobal" data-wb-path="pool.max_in_flight_global" data-wb-type="number" min="1" placeholder="2"></div>
          <div class="wb-field"><label for="wbBreakerThreshold">Breaker threshold</label><input id="wbBreakerThreshold" data-wb-path="pool.breaker_threshold" data-wb-type="number" min="1" placeholder="3"></div>
          <div class="wb-field"><label for="wbSoftRate">Soft rate cooldown</label><input id="wbSoftRate" data-wb-path="cooldown.soft_rate" data-wb-type="duration" placeholder="600s"></div>
          <div class="wb-field"><label for="wbSoftMax">Soft cooldown max</label><input id="wbSoftMax" data-wb-path="cooldown.soft_rate_max" data-wb-type="duration" placeholder="2h"></div>
          <div class="wb-field"><label for="wbBreakerCooldown">Breaker cooldown</label><input id="wbBreakerCooldown" data-wb-path="pool.breaker_cooldown" data-wb-type="duration" placeholder="30m"></div>
          <div class="wb-field"><label for="wbBreakerMax">Breaker cooldown max</label><input id="wbBreakerMax" data-wb-path="pool.breaker_cooldown_max" data-wb-type="duration" placeholder="6h"></div>
          <div class="wb-field"><label for="wbDegradeThreshold">Degrade threshold</label><input id="wbDegradeThreshold" data-wb-path="pool.degrade_threshold" data-wb-type="number" min="1" placeholder="5"></div>
          <div class="wb-field"><label for="wbDegradeCooldown">Degrade cooldown</label><input id="wbDegradeCooldown" data-wb-path="pool.degrade_cooldown" data-wb-type="duration" placeholder="10m"></div>
          <div class="wb-field"><label for="wbDegradeMax">Degrade cooldown max</label><input id="wbDegradeMax" data-wb-path="pool.degrade_cooldown_max" data-wb-type="duration" placeholder="2h"></div>
          <div class="wb-field"><label for="wbIdleWeight">Idle weight / hour</label><input id="wbIdleWeight" data-wb-path="pool.idle_weight_per_hour" data-wb-type="number" step="0.1" placeholder="0.5"></div>
          <div class="wb-field"><label for="wbIdleMax">Idle weight max</label><input id="wbIdleMax" data-wb-path="pool.idle_weight_max" data-wb-type="number" step="0.1" placeholder="5"></div>
          <div class="wb-field"><label for="wbCostExplore">Cost explore window</label><input id="wbCostExplore" data-wb-path="pool.cost_explore_interval" data-wb-type="duration" placeholder="30m"></div>
          <div class="wb-field"><label for="wbStickyTTL">Sticky TTL</label><input id="wbStickyTTL" data-wb-path="session_sticky.ttl" data-wb-type="duration" placeholder="30m"></div>
        </div>

        <div class="wb-config-group">
          <h3>Session and upstream</h3>
          <div class="wb-field"><label for="wbTimeout">Short request timeout</label><input id="wbTimeout" data-wb-path="upstream.timeout_seconds" data-wb-type="number" min="1" placeholder="120"></div>
          <div class="wb-field"><label for="wbHeaderTimeout">First-byte timeout</label><input id="wbHeaderTimeout" data-wb-path="upstream.header_timeout_seconds" data-wb-type="number" min="1" placeholder="120"></div>
          <div class="wb-field"><label for="wbIdleTimeout">Stream idle timeout</label><input id="wbIdleTimeout" data-wb-path="upstream.idle_timeout_seconds" data-wb-type="number" min="1" placeholder="300"></div>
          <div class="wb-field"><label for="wbUserAgent">User-Agent</label><input id="wbUserAgent" data-wb-path="upstream.user_agent" data-wb-send-empty="1" placeholder="Leave empty = CLI / 2.63.2 CodeBuddy / 2.63.2"></div>
          <div class="wb-field">
            <label for="wbPromptMode">System prompt mode</label>
            <select id="wbPromptMode" data-wb-path="prompt.mode">
              <option value="custom">custom — gateway-owned prompt (avoids fingerprint false positives)</option>
              <option value="append">append — insert gateway prompt after client system (both apply)</option>
              <option value="passthrough">passthrough — pass client system through unchanged</option>
            </select>
          </div>
          <div class="wb-field"><label for="wbPromptFile">Prompt file</label><input id="wbPromptFile" data-wb-path="prompt.file" data-wb-send-empty="1" placeholder="Leave empty = built-in default prompt"></div>
          <div class="wb-switch"><input type="checkbox" id="wbSanitize" data-wb-path="features.sanitize_blacklist_fingerprints"><label for="wbSanitize">Sanitize outbound fingerprints</label></div>
          <div class="wb-switch"><input type="checkbox" id="wbStickyEnabled" data-wb-path="session_sticky.enabled"><label for="wbStickyEnabled">Sticky session routing</label></div>
          <div class="hint" style="margin-top:10px">Upstash Redis mirror, auth dir, and state file are edited in the config file manually (restart items).</div>
        </div>
      </div>
      <div class="form-actions" style="justify-content:flex-end">
        <button type="button" class="btn" onclick="loadWorkbuddyConfig()">Discard changes</button>
        <button type="submit" class="btn btn-primary" id="wbSaveConfig">Save configuration</button>
      </div>
    </div>
  </div>
</form>
</div>

</div>
</div>

<div id="toast" class="toast" popover="manual"></div>

<dialog id="wbAddDialog">
  <div class="wb-dialog-head">
    <span>Add WorkBuddy account</span>
    <button class="btn btn-sm" onclick="closeWorkbuddyAdd()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="form-row" style="margin-bottom:8px">
      <div class="field">
        <label>Realm</label>
        <select id="wbRealm"><option value="cn">CN (China)</option><option value="global">Global (International)</option></select>
      </div>
    </div>
    <div class="hint" id="wbAddStatus">Choose a realm and request an authorization link.</div>
    <div class="wb-link-row">
      <input type="text" id="wbOAuthUrl" readonly placeholder="authorization URL">
      <button class="btn btn-sm" onclick="copyWorkbuddyOAuthUrl()">Copy</button>
      <button class="btn btn-sm btn-primary" onclick="openWorkbuddyOAuthUrl()">Open</button>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="wbStartLogin">Get authorization link</button>
      <button class="btn" id="wbImportJson" onclick="document.getElementById('wbDialogImportFile').click()">Import JSON</button>
    </div>
    <input type="file" id="wbDialogImportFile" accept=".json" hidden onchange="importWorkbuddyAccounts(this)">
  </div>
</dialog>

<dialog id="wbDetailsDialog" class="wb-wide">
  <div class="wb-dialog-head">
    <span>Points breakdown <span class="auto-pill" id="wbDetailsWho"></span></span>
    <button class="btn btn-sm" onclick="closeWorkbuddyDetails()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="hint" id="wbDetailsStatus">Loading...</div>
    <div id="wbDetailsBody"></div>
  </div>
</dialog>

<dialog id="accAddDialog">
  <div class="wb-dialog-head">
    <span>Add account</span>
    <button class="btn btn-sm" onclick="closeAccAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="accAddHint">The type is detected automatically — sk_... is pooled as a static API key, anything else as an OAuth refresh token.</p>
    <div class="field">
      <label>Token or API key *</label>
      <input type="text" id="accAddToken" placeholder="workos-... refreshToken or sk_... API key" style="font-family:'JetBrains Mono',Consolas,monospace">
    </div>
    <div class="field">
      <label>Email (optional)</label>
      <input type="text" id="accAddEmail" placeholder="user@example.com">
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="accAddSubmit" onclick="accAddFromDialog()">Add account</button>
      <button class="btn" onclick="closeAccAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="zenKeyAddDialog">
  <div class="wb-dialog-head">
    <span>Add credentials</span>
    <button class="btn btn-sm" onclick="closeZenKeyAddDialog()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <p class="hint" style="margin-top:0" id="zenKeyAddHint">One OpenCode API key per line; use "public" for anonymous access.</p>
    <div class="field">
      <label>API keys (one per line) *</label>
      <textarea id="zenKeyAddInput" rows="5" placeholder="public" style="font-family:'JetBrains Mono',Consolas,monospace"></textarea>
    </div>
    <div class="wb-dialog-actions">
      <button class="btn btn-primary" id="zenKeyAddSubmit" onclick="zenKeyAddFromDialog()">Save</button>
      <button class="btn" onclick="closeZenKeyAddDialog()">Cancel</button>
    </div>
  </div>
</dialog>

<dialog id="wbTodoDialog" class="wb-wide">
  <div class="wb-dialog-head">
    <span>Task center&#x20;</span>
    <button class="btn btn-sm" onclick="closeWorkbuddyTodoScan()">Close</button>
  </div>
  <div class="wb-dialog-body">
    <div class="justify-between">
      <div class="hint" id="wbTodoStatus">Pick a concurrency, then click "Scan tasks"</div>
      <div class="wb-task-actions">
        <label class="inline-flex" style="font-size:12px;color:var(--text2)">Concurrency
          <select id="wbTodoConcurrency" style="width:64px">
            <option value="1" selected>1</option>
            <option value="2">2</option>
            <option value="3">3</option>
          </select>
        </label>
        <button class="btn btn-sm" id="wbTodoScan" onclick="scanWorkbuddyTodos()">Scan tasks</button>
        <button class="btn btn-sm btn-primary" id="wbTodoRunAll" onclick="runWorkbuddyAllTodos()">Run all</button>
      </div>
    </div>
    <div id="wbTodoBody" style="margin-top:12px"></div>
  </div>
</dialog>

<script>
const API = '/admin/api';

// ========== Theme toggle ==========
function getTheme() {
  return localStorage.getItem('theme') || 'dark';
}
function applyTheme(t) {
  if (t === 'dark') {
    document.documentElement.setAttribute('data-theme', 'dark');
    const ic = document.getElementById('themeIcon');
    if (ic) ic.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
  } else {
    document.documentElement.setAttribute('data-theme', 'light');
    const ic = document.getElementById('themeIcon');
    if (ic) ic.innerHTML = '<svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';
  }
}
function toggleTheme() {
  const cur = getTheme();
  const next = cur === 'dark' ? 'light' : 'dark';
  localStorage.setItem('theme', next);
  applyTheme(next);
}
applyTheme(getTheme());

const _ = id => document.getElementById(id);
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtNum = n => (n || 0).toLocaleString('en-US');
// fmtWhen 把服务器发来的 RFC3339 时刻渲染成浏览器的本地时间（同一时刻在不同
// 时区看到各自的钟点）。容器时区是 UTC，直接显示服务器格式化的读数会和本地
// 时间差一个时差；解析失败则原样返回，至少不丢信息。
// 零值时间（Go 的 time.Time{} = 0001-01-01T00:00:00Z）视为"没有这个时刻"：
// 它是 truthy，会被 new Date 解析成年份 1 并渲染成 "1/1/1, 12:00:00 AM"。
const fmtWhen = s => {
  if (!s || String(s).startsWith('0001-')) return '';
  const d = new Date(s);
  return isNaN(d.getTime()) ? s : d.toLocaleString('en-US');
};
const fmtTokens = n => {
  n = n || 0;
  if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.?0+$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
};
// ========== i18n（English / 简体中文） ==========
// 机制：字典以英文原文为键。静态 HTML 与 JS 渲染出的纯文本节点由
// MutationObserver 即时翻译（渲染代码无需逐处包翻译函数）；与变量拼接的
// 字符串在渲染处用 T() 包住英文片段；confirm() 原生对话框只能显式 T()。
// 切换语言 = 保存 localStorage 偏好 + location.reload()（避免维护反向映射，
// 也保证重载后静态文本从首次渲染起就是目标语言）。
let LANG = localStorage.getItem('lang') === 'zh' ? 'zh' : 'en';
const I18N_ZH = {
  // WorkBuddy native admin console
  'Original panel': '原版入口',
  'Task center': '任务中心',
  'Refresh all': '全部刷新',
  'Refresh all account balances': '刷新全部账号的余额',
  'Stage: clear proxy bindings on ALL WorkBuddy accounts and disable them — click Save to apply': '清空全部账号的代理绑定并置为禁用，点击保存后生效',
  'Check in all': '全部签到',
  'Pet patrol': '宠物巡检',
  'Activity all': '连登活跃',
  'Refresh login': '刷新登录',
  'Refresh': '刷新',
  'Account': '账号',
  'Points': '积分',
  'Import JSON': '导入 JSON',
  'No accounts yet': '暂无账号信息,请添加',
  'Failed to load WorkBuddy accounts: ': '加载 WorkBuddy 账号失败：',
  'Credits': '额度',
  'Success / errors': '成功 / 错误',
  'Last success': '最近成功',
  'Check-in': '签到',
  'Ready': '就绪',
  'Authenticated': '已认证',
  'Cooling': '冷却中',
  'Suspended': '已暂停',
  'Disabled': '已停用',
  'Deleted': '已删除',
  'Unknown': '未知',
  'accounts': '账号',
  'tok': 'token',
  'Balances refreshed': '余额已刷新',
  'Get authorization link': '获取授权链接',
  'Choose a realm and request an authorization link.': '选择区域并请求授权链接。',
  'Requesting authorization link...': '正在请求授权链接...',
  'Open the link and complete the browser login.': '打开链接并完成浏览器登录。',
  'Authorization session created.': '授权会话已创建。',
  'WorkBuddy account added': 'WorkBuddy 账号已添加',
  'Authorization link copied': '授权链接已复制',
  'Copy failed': '复制失败',
  // 拖拽排序
  'Save or discard staged edits before reordering': '有未保存的暂存修改，请先保存或放弃后再拖拽排序',
  'Order saved': '顺序已保存',
  'Reorder failed: ': '排序失败：',
  'loaded': '已加载',
  'load failed': '加载失败',
  'WorkBuddy config load failed: ': 'WorkBuddy 配置加载失败：',
  'WorkBuddy config saved': 'WorkBuddy 配置已保存',
  'WorkBuddy config saved (': 'WorkBuddy 配置已保存 (',
  ' restart items)': ' 个重启项)',
  'WorkBuddy config save failed: ': 'WorkBuddy 配置保存失败：',
  'WorkBuddy': 'WorkBuddy',
  'Add WorkBuddy account': '添加 WorkBuddy 账号',
  'Close': '关闭',
  'Realm': '区域',
  'authorization URL': '授权 URL',
  'Copy': '复制',
  // 侧边导航 / 分组
  'Account pool': '账号池',
  'Services': '服务',
  'Dashboard': '仪表盘',
  'Gateway settings': '网关设置',
  'Request logs': '请求日志',
  'Proxy pool': '代理池',
  'Custom aliases': '自定义别名',
  'Light': '亮色',
  'Dark': '暗色',
  'Toggle theme': '切换主题',
  // 仪表盘
  'Platform overview': '平台概览',
  'Click a platform card to open its management page.': '点击平台卡片进入对应管理页。',
  'Request statistics': '请求统计',
  'Counted since ': '统计自 ',
  'requests': '请求',
  'errors': '错误',
  'Avg ': '平均 ',
  'Active': '活跃',
  'Cooldown': '冷却中',
  'Expired': '已失效',
  'Add account': '添加账号',
  'Endpoints': '接口',
  'OpenAI Chat format — works with most tools out of the box': 'OpenAI Chat 格式 —— 大多数工具开箱即用',
  'Anthropic Messages format — for Claude Code / Cline and similar clients': 'Anthropic Messages 格式 —— 供 Claude Code / Cline 等客户端使用',
  'OpenAI Responses format — for Cursor and similar clients': 'OpenAI Responses 格式 —— 供 Cursor 等客户端使用',
  'Model list': '模型列表',
  // 账号页
  'Add': '添加帐号',
  'Test all': '批量测试',
  'Disable all': '一键禁用',
  'No changes to save': '没有需要保存的修改',
  'Save failed for ': '保存失败 ',
  ' account(s)': ' 个账号',
  ' account change(s)': ' 处修改',
  'No accounts to test': '没有可测试的账号',
  'Batch test done: ': '批量测试完成：',
  ' tested, ': ' 个已测，',
  ' ok': ' 可用',
  ' cooling': ' 冷却',
  ' error': ' 异常',
  'Participates in request routing': '参与请求路由',
  'Discard': '复位',
  'Discard staged changes and reload the last saved state': '撤销暂存的修改，恢复为上次保存的状态',
  'Add credentials': '添加凭据',
  'Confirm': '确认',
  'keys': '个 key',
  'API keys (one per line) *': 'API key（每行一个）*',
  'One OpenCode API key per line; use "public" for anonymous access.': '每行一个 OpenCode API key；匿名访问填 public。',
  'Enter at least one key': '请至少输入一个 key',
  'No new keys (all already in the pool)': '没有新 key（均已在池中）',
  'Added ': '已添加 ',
  ' key(s)': ' 个 key',
  'No keys to test': '没有可测试的 key',
  ' (deleted)': '（已删除）',
  'Running': '执行中',
  'Finished': '执行结束',
  'CN (China)': 'CN (国内版)',
  'Global (International)': 'Global (国际版)',
  'Points breakdown': '积分构成',
  'Pick a concurrency, then click "Scan tasks"': '选择并发数后点击“扫描任务”',
  'Concurrency': '并发',
  'Scan tasks': '扫描任务',
  'Run all': '全部执行',
  'Bound egress no longer in the proxy pool — isolation logic skips this account': '绑定的出口已不在代理池中，该账号会被隔离逻辑跳过',
  'Main egress: requests go through this egress whenever available': '主出口：可用时固定从该出口发出',
  'Backup egress: fallback while the main egress is cooling down or unavailable': '备用出口：主出口冷却或不可用时兜底',
  'Failed to save ': '保存失败 ',
  ' account edits': ' 个账号的修改',
  'Clear proxy bindings on ALL accounts and disable them? Takes effect when you click Save.': '清空全部账号的代理绑定并置为禁用？点击"保存"后生效。',
  'Delete this WorkBuddy account and its credentials?': '删除该 WorkBuddy 账号及其凭证？',
  '(unnamed)': '(未命名)',
  'Querying points breakdown from upstream...': '正在向上游查询积分构成...',
  'No points data found for this account': '未找到该账号的积分数据',
  'Live from upstream · ': '实时查询上游 · ',
  ' points packages': ' 个积分包',
  'Failed to read: ': '读取失败：',
  'Query failed: ': '查询失败：',
  'first issued ': '首发 ',
  'Total ': '共 ',
  ' package(s)': ' 个包',
  'Remaining points · used ': '剩余积分 · 已用 ',
  'Package / source': '包名 / 来源',
  'Face value': '面额',
  'Remaining': '剩余',
  'Used': '已用',
  'Granted': '发放',
  'Expires': '到期',
  'No points packages': '没有积分包',
  'No pending tasks for all accounts': '全部账号没有待办任务',
  'Energy': '能量',
  'Task: ': '任务：',
  'Description: ': '说明：',
  'Progress: ': '进度：',
  'Reward: ': '奖励：',
  'Result: ': '结果：',
  'Pending': '待执行',
  'Scan failed: ': '扫描失败：',
  ' item(s)': ' 项',
  'Scanning with concurrency ': '正在按并发 ',
  ' scanning tasks...': ' 扫描任务...',
  'Scanning...': '扫描中...',
  'Scan complete: ': '扫描完成：',
  ' pending item(s)': ' 项待办',
  'Claimed': '已领取',
  'In progress': '进行中',
  'Done': '完成',
  'Skipped': '跳过',
  'Failed': '失败',
  ': ': '：',
  'Failed to read queue: ': '读取队列失败：',
  'Run all listed tasks with concurrency ': '按并发 ',
  ' (tasks run serially within each account, auto-claim enabled). Confirm?': ' 执行列表中的全部任务（账号内串行，自动领取任务）。确认继续？',
  'No pending tasks': '没有待办任务',
  'Queue started: ': '队列已启动：',
  ' item(s) (concurrency ': ' 项（并发 ',
  'Queue started, running...': '队列已启动，正在执行...',
  'Failed to run: ': '执行失败：',
  'Saving...': '保存中...',
  'free': '免费',
  'Default tier': '默认档',
  'No WorkBuddy models under the current filter': '当前筛选下没有 WorkBuddy 模型',
  'WorkBuddy models': 'WorkBuddy 模型',
  'Filter': '筛选',
  'Model rate cap for WorkBuddy models. Applies to the model list and to GET /v1/models responses; saves immediately.': 'WorkBuddy 模型倍率上限。同时作用于模型列表与 GET /v1/models 返回结果，保存后立即生效。',
  'Model rate filter': '模型倍率筛选',
  '0 — free only': '0 — 仅免费',
  'WorkBuddy configuration': 'WorkBuddy 配置',
  'Managed by the admin session': '面板由当前管理员会话管理',
  'Listening, credentials, and data paths stay under the gateway admin session.': '监听地址、API 密钥、凭证目录与状态文件路径由当前项目统一管理。',
  'Scheduled jobs': '定时任务',
  'Automatic check-in': '自动签到',
  'Check-in hours': '签到时点（小时，逗号分隔）',
  'Automatic patrol': '自动巡检',
  'Patrol hours': '旅行时点（小时，逗号分隔）',
  'Automatic activity': '自动连登',
  'Activity hours': '上报时点（小时，逗号分隔）',
  'Automatic keepalive': '自动保活',
  'Keepalive hours': '保活时点（小时，逗号分隔）',
  'Background balance refresh': '后台刷新余额',
  'Refresh interval (min)': '刷新间隔（分钟）',
  'Growth tasks': '成长任务自动执行',
  'Growth hours': '执行时点（小时，逗号分隔）',
  'Pool and cooldown': '账号池与流量治理',
  'Max in-flight / account': '单账号最大在途',
  'Global in-flight limit': '国际版在途上限',
  'Breaker threshold': '连续失败熔断阈值',
  'Soft rate cooldown': '软限流冷却基数',
  'Soft cooldown max': '软冷却退避上限',
  'Breaker cooldown': '熔断基础时长',
  'Breaker cooldown max': '熔断退避上限',
  'Degrade threshold': '连败降权阈值',
  'Degrade cooldown': '连败降权时长',
  'Degrade cooldown max': '连败降权上限',
  'Idle weight / hour': '闲置补偿 / 小时',
  'Idle weight max': '闲置补偿上限',
  'Cost explore window': '成本探索窗口',
  'Sticky TTL': '会话粘性 TTL',
  'Session and upstream': '上游与高级',
  'Short request timeout': '短请求超时',
  'First-byte timeout': '聊天首字节超时',
  'Stream idle timeout': '流空闲超时',
  'User-Agent': '出站 User-Agent',
  'Leave empty = CLI / 2.63.2 CodeBuddy / 2.63.2': '留空 = CLI/2.63.2 CodeBuddy/2.63.2',
  'System prompt mode': '系统提示词模式',
  'custom — gateway-owned prompt (avoids fingerprint false positives)': 'custom — 网关自有提示词（避免指纹误报）',
  'append — insert gateway prompt after client system (both apply)': 'append — 客户端 system 后插网关提示词（并用）',
  'passthrough — pass client system through unchanged': 'passthrough — 透传客户端原始 system',
  'Prompt file': '提示词文件路径',
  'Leave empty = built-in default prompt': '留空 = 内置默认提示词',
  'Sanitize outbound fingerprints': '出站请求指纹脱敏',
  'Sticky session routing': '会话粘性路由',
  'Upstash Redis mirror, auth dir, and state file are edited in the config file manually (restart items).': 'Upstash Redis 镜像、凭证目录与状态文件路径需手工编辑配置文件（判为重启项）。',
  'Discard changes': '放弃修改',
  'Save configuration': '保存配置',
  ' (was: ': '（原状态：',
  'Reset failed: ': '恢复失败：',
  'Key #': 'key #',
  ' (': '（',
  ') — ': '）— ',
  'cooldown until ': '冷却至 ',
  ' (seed)': '（种子）',
  ' failed': ' 失败',
  ' minted': ' 成功',
  'Email (optional)': '邮箱（可选）',
  'Cancel': '取消',
  'The type is detected automatically — sk_... is pooled as a static API key, anything else as an OAuth refresh token.': '类型自动识别：sk_... 按静态 API key 入池，其余按 OAuth refreshToken 处理。',
  ', ': '，',
  'Clear proxy bindings on ALL keys and disable routing? Click Save to apply.': '清空全部 key 的代理绑定并禁用路由？点击「保存」后生效。',
  'Stage: clear proxy bindings on ALL keys and disable routing — click Save to apply': '暂存操作：清空全部 key 的代理绑定并禁用路由，点击「保存」后生效',
  'Run the Test probe on every account (same as clicking Test on each row)': '对当前平台所有账号执行真实探测（等价于逐行点一次测试）',
  'Email': '邮箱',
  'Today/Total tokens': '今日/累计 token',
  'Last used': '最近使用',
  'Created': '创建时间',
  'Proxy binding': '代理绑定',
  'Actions': '操作',
  'Loading...': '加载中...',
  'are counted locally by this proxy (input+output; exact when upstream returns usage, otherwise estimated from the request body) to gauge headroom before official rate limits. The Test button sends a real probe request. The Reset button': '由本代理本地统计（输入+输出；上游返回 usage 时精确，否则按请求体估算），用于判断距离官方限流还有多少余量。Test 按钮发送一次真实探测请求。Reset 按钮 ',
  'probes upstream rate-limit status': '探测上游限流状态',
  ": if still limited it stays in cooldown and shows the estimated recovery time; only a passing probe lifts the cooldown and resets today's stats.": '：若仍在限流则保持冷却并显示预计恢复时间；只有探测通过才会解除冷却并重置今日统计。',
  '(isolation mode, on by default): a bound account always egresses via its main proxy, falls back to the backup, and is': '（隔离模式，默认开启）：绑定的账号永远从主代理出网，主不可用落到辅代理；',
  'skipped entirely': '完全跳过',
  'while both are unavailable — never routed through another exit. Configure the list on the': '两者都不可用时整个账号被跳过 —— 绝不路由到其他出口。代理列表在',
  'page.': ' 页配置。',
  'Counted locally by this proxy — not the official free quota': '本代理本地统计 —— 非官方免费额度',
  'Main / backup egress — isolation mode picks main first, then backup; both down = account skipped': '主/辅出口 —— 隔离模式先用主、再用辅；双不可用 = 跳过该账号',
  'Main / backup egress — isolation mode picks main first, then backup; both down = key skipped. The harvester also mints through the bound exit.': '主/辅出口 —— 隔离模式先用主、再用辅；双不可用 = 跳过该 key。收割机铸造也走绑定出口。',
  'Main exit — used whenever available': '主出口 —— 可用即优先',
  'Backup exit — used when main is cooling/removed': '辅出口 —— 主冷却/被删时使用',
  'Bound proxy is no longer in the proxy pool — the account is skipped until fixed': '绑定的代理已不在代理池中 —— 修复前该账号一直被跳过',
  'Bound proxy is no longer in the proxy pool — the key is skipped until fixed': '绑定的代理已不在代理池中 —— 修复前该 key 一直被跳过',
  // 添加账号弹窗（accAddDialog，Cline 页"添加账号"按钮）
  'Token or API key *': 'Token 或 API key *',
  // 代理池页
  'Egress proxies': '出口代理',
  'One proxy list serves every upstream that opts in below. One proxy per line:': '一份代理列表供下方所有选择启用的上游共用。每行一条：',
  'or': '或',
  '. Requests rotate across healthy proxies per request; a proxy that hits a rate limit is cooled down and skipped automatically.': '。请求按次在健康代理间轮转；触发限流的代理会自动冷却并被跳过。',
  'one per line: socks://b64(user:pass)@host:port#alias, socks5://user:pass@host:port#alias or http://host:port': '每行一条：socks://b64(账号:密码)@host:port#别名、socks5://账号:密码@host:port#别名 或 http://host:port（#别名自动识别）',
  'Proxy list': '代理列表',
  'Proxy selection strategy': '代理选择策略',
  'Round-robin (round_robin)': '轮询（round_robin）',
  'Random (random)': '随机（random）',
  'Fill (fill)': '填满优先（fill）',
  'Save proxy pool': '保存代理池',
  'Global policy': '全局策略',
  'Cline': 'Cline',
  'Use proxy pool': '走代理池',
  'Proxy': '代理',
  'Proxy health (online rate)': '代理健康（在线率）',
  'Attempts': '尝试数',
  'Rate': '在线率',
  'Recent': '最近',
  'Last OK': '最近成功',
  'Last Fail': '最近失败',
  'No proxies configured': '未配置代理',
  'Upstream attempts that egressed via this proxy since the pool was last edited': '上次编辑代理池以来，经此代理出网的上游尝试次数',
  'Transport-layer success rate since the pool was last edited (upstream 4xx/5xx still counts as the proxy being online)': '上次编辑代理池以来的传输层成功率（上游 4xx/5xx 不算代理失败）',
  'Success rate over the most recent attempts (window: last 50). Below 80% with at least 10 samples is highlighted red — time to replace the proxy.': '最近若干次尝试的成功率（窗口：最近 50 次）。样本 ≥10 且低于 80% 标红——该考虑换掉这个代理了。',
  'Rates are passively sampled from real upstream traffic only — no probing traffic is generated. "No data" means this exit has not been used since the pool was last edited (with the fill strategy only the first proxy gets traffic; global switches and per-identity bindings decide the rest). Editing the proxy list resets all counters.': '在线率只从真实上游流量被动采样，不产生任何探测流量。“无数据”表示该出口自上次编辑代理池以来没被用到（fill 策略只有第一个代理会拿到流量，其余由全局开关与各身份的绑定决定）。编辑代理列表会清空全部统计。',
  'Restore defaults': '恢复默认',
  'Restore the default Cline CLI headers': '恢复默认的 Cline CLI 请求头',
  'Headers restored to defaults': '请求头已恢复默认',
  'OpenCode keys without proxy bindings follow this switch.': 'OpenCode 未绑定代理的 key 遵循此开关。',
  'Clear bindings': '清空代理',
  'Apply to others': '代理套用',
  'Copy this page\'s proxy bindings by row order to the other two account pools. Takes effect immediately; only proxy bindings are changed, enable/disable states are untouched.': '把本页的代理绑定按行序复制到其余两个账号池（多出的丢弃，不足的保留原样）。即刻生效，只改代理绑定，不影响启用/禁用状态。',
  'No accounts to apply from': '当前页没有账号可套用',
  'Apply ': '把 ',
  ' proxy bindings by row order to ': ' 的代理绑定按行序套用到 ',
  ' and ': ' 和 ',
  ' accounts? Takes effect immediately; ': ' 账号？即刻生效；',
  'only proxy bindings change, enable/disable states are untouched.': '只改代理绑定，不改启用/禁用状态。',
  'Applied ': '已套用 ',
  ' binding(s) to other pools': ' 条绑定到其他账号池',
  ' binding(s), ': ' 条绑定，成功 ',
  ' failed': ' 条失败',
  'Enable all': '一键启用',
  'Direct connection': '直连',
  'Save': '保存',
  'Cline accounts without proxy bindings follow this switch.': 'Cline 未绑定代理的账号遵循此开关。',
  'Delete': '删除',
  'Test': '测试',
  'Reset': '重置',
  'Open': '打开',
  'Tokens': 'Token 用量',
  'Admin panel:': '管理面板：',
  'API address:': 'API 地址：',
  'OpenCode config saved': 'OpenCode 配置已保存',
  'Config export / import': '配置导入导出',
  'Export config': '导出配置',
  'Import config': '导入配置',
  'Import/export all persisted configuration. The export file contains private information; keep it safe. Import is an overwrite operation, so proceed with caution.': '导入/导出所有持久化配置。导出配置含私密信息，请妥善保管。 导入配置为覆盖操作，请谨慎执行。',
  'Config exported': '配置已导出',
  'Config imported': '配置已导入',
  'Unsupported backup version': '不支持的备份版本',
  'Choose a backup JSON file first': '请先选择备份 JSON 文件',
  'Invalid backup file: ': '备份文件无效：',
  'Import replaces sections present in the file (accounts / keys / proxies / combos ...). Continue?': '导入将用文件内容替换当前对应的配置段（Cline 账号 / 客户端 key / OpenCode key / 代理池 / 自定义别名等，文件中存在的段都会替换）。继续？',
  'State': '状态',
  'Global': '全局',
  'Follows the main slot': '跟随主出口',
  'No matching proxies': '没有匹配的代理',
  'Proxy isolation (identity ↔ exit binding)': '代理隔离（身份 ↔ 出口绑定）',
  'Isolation mode': '隔离模式',
  'Enabled (default) — bound identities only ever use their bound exits': '启用（默认）—— 绑定的身份只从绑定出口出网',
  'Disabled — ignore bindings; exits rotate per the strategy above (direct when pool off)': '关闭 —— 忽略绑定；出口按上方轮转策略轮换（未启用代理池时直连）',
  'With isolation on, every identity (account or key) only ever egresses through its bound exits — main proxy first, backup as fallback; when both are unavailable the identity is skipped, never falling back to another exit or a direct connection. The': '隔离开启时，每个身份只从自己绑定的出口出网：主代理优先，辅代理兜底；两者都不可用时该身份会被整体跳过 —— 绝不回退到其他出口或直连。',
  'env var only accepts true/false: true forces this on (read-only toggle); false just changes the default to off.': ' 仅接受 true/false：true 强制开启（开关只读）；false 仅把默认值改为关闭(仍可修改)。',
  'PROXY_ISOLATION=true is set — isolation is forced on. Unset the env var (or set it to false) to control isolation from the panel.': '已设置 PROXY_ISOLATION=true —— 隔离被强制开启。删除该环境变量（或设为 false）才能在面板控制隔离。',
  'Enable backup proxies': '启用辅代理',
  'Off by default: every identity egresses through its main proxy only — when the main proxy is cooling down or removed, the identity is skipped for that round, never trying the backup and never falling back to a direct connection. When enabled, backups act as the fallback for the main proxy to keep identities available as much as possible.': '辅代理功能默认关闭，每个身份只从主代理出网——主代理冷却或被删除时，本轮直接跳过该身份，不会尝试辅代理，也绝不回退直连。辅代理功能开启时，辅代理作为主代理备份，尽可能保证身份可用。',
  'Backup proxies are disabled — follows the main slot': '辅代理已关闭 —— 跟随主代理',
  // 网关设置页
  'API keys': 'API key',
  'Generated keys authenticate client access to the proxy API; they only take effect when the API_KEY environment variable is not set.': '生成的 key 用于客户端调用代理 API 鉴权，该 key 仅在未设置 API_KEY 环境变量时生效。',
  'Generate new key': '生成新 key',
  'Available models': '可用模型',
  'Cline free models': 'Cline 免费模型',
  'OpenCode free models': 'OpenCode 免费模型',
  'Refresh models': '刷新模型',
  'Auto-syncs the official model feeds from upstreams (60s)': '自动同步上游的官方模型源（60 秒）',
  'General config': '常规配置',
  'Listen address': '监听地址',
  'Default model': '默认模型',
  'Account scheduling strategy': '账号调度策略',
  'Record admin logs': '记录管理日志',
  'Admin access logging enabled': '已开启管理页访问日志',
  'Admin access logging disabled': '已关闭管理页访问日志',
  'Request headers (mimicking the Cline CLI)': '请求头（模拟 Cline CLI）',
  'Header': '请求头',
  'Value': '值',
  'Add header': '添加请求头',
  'Save headers': '保存请求头',
  'These headers are attached to every request forwarded to the Cline API to mimic the official client.': '这些请求头会附加在每个转发到 Cline API 的请求上，用于模拟官方客户端。',
  'Danger zone': '危险区',
  'Delete all data': '删除全部数据',
  'Deletes all persisted data, including but not limited to accounts and configuration. This cannot be undone; proceed with caution!': '删除全部持久化数据，包含但不限于账号和配置等，操作不可撤销，请谨慎操作！',
  'Delete ALL persisted data? This will clear Cline accounts and client keys, OpenCode keys, WorkBuddy accounts, the proxy pool, default model, request headers, scheduling strategy, custom aliases, sticky sessions, and request logs. This cannot be undone!': '确定删除全部持久化数据？将清空 Cline 账号与客户端 key、OpenCode key、WorkBuddy 账号、代理池、默认模型、请求头、调度策略、自定义别名、粘性会话和请求日志，且不可撤销！',
  'no charge': '免费',
  // 请求日志页
  'last 500 entries, persisted to data/requests.jsonl': '最近 500 条，持久化到 data/requests.jsonl',
  'Auto-refreshing': '自动刷新中',
  'Paused': '已暂停',
  'Pause': '暂停',
  'Resume': '继续',
  'Time': '时间',
  'Source': '来源',
  'Method': '方法',
  'Path': '路径',
  'Model': '模型',
  'Route': '路由',
  'Upstream': '账号/Key',
  'Main': '主',
  'Backup': '辅',
  'Direct': '直连',
  'Status': '状态',
  'Duration': '耗时',
  'No requests logged': '暂无请求记录',
  'cline pool': 'cline 池',
  'admin': '管理',
  'meta': '元数据',
  'other': '其他',
  // Combos 页
  'Create custom alias': '创建自定义别名',
  'Clients request the alias ID as their model, and the proxy rewrites it to the target model for the chosen platform. Alias IDs are user-defined (e.g.': '客户端以别名 ID 作为 model 请求，代理将其改写为所选平台上的目标模型。别名 ID 自定义（如 ',
  ') and must not collide with real model IDs; the target must belong to the same platform — cross-platform targets are not allowed.': '）且不得与真实模型 ID 冲突；目标必须属于同一平台 —— 不允许跨平台。',
  'Alias ID': '别名 ID',
  'Platform': '平台',
  'Existing custom aliases': '现有自定义别名',
  'Target model': '目标模型',
  'No combos yet — create one with the form above.': '还没有自定义别名 —— 用上方表单创建一个。',
  'No models on this platform': '该平台上没有可用模型',
  'Failed to load model list': '模型列表加载失败',
  'socks5 pool': 'socks5 池',
  // opencode 页
  'OpenCode': 'OpenCode',
  'On': '开',
  'Off': '关',
  'Probe model (used by the Test buttons):': '探测模型（Test 按钮使用）：',
  'auto — big-pickle first, then live models': '自动 —— big-pickle 优先，其次 live 模型',
  'Key': 'Key',
  'Usage': '用量',
  'Session': '会话',
  'live': 'live',
  'stale': '已失效',
  'not minted': '未铸造',
  'no key': '无 key',
  'cooling': '冷却中',
  ' · until ': ' · 至 ',
  'Base URL': 'Base URL',
  'Rate-limit defense': '限流防御',
  'Max concurrency': '最大并发',
  'Rate-limit retries': '限流重试次数',
  'Save rate-limit config': '保存限流配置',
  'Context compaction (OpenCode official mechanism)': '上下文压缩（OpenCode 官方机制）',
  'Auto-compaction': '自动压缩',
  'Reserved buffer': '预留缓冲',
  'Tail tokens kept': '尾部保留 token',
  'Summary model': '摘要模型',
  'empty = same as request model': '留空 = 与请求模型相同',
  'Summary cap': '摘要上限',
  'Save compaction config': '保存压缩配置',
  'Live session IDs (OpenCode FreeTier gate)': 'Live 会话 ID（OpenCode FreeTier 门槛）',
  'The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session': '免费层只接受上游真实见过的会话 ID（由 OpenCode CLI 铸造）。没有 live 会话的 key ',
  'always': '必定',
  'The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session <b>always</b> fails with 403.': '免费层只接受上游真实见过的会话 ID（由 OpenCode CLI 铸造）。没有 live 会话的 key <b>必定</b> 403 失败。',
  'Auto-refresh every {h}h (kept below the 5h quota window), minting {n} key(s) at a time': '每 {h} 小时自动刷新一次（保持在 5 小时配额窗口内），每次并行铸造 {n} 个 key',
  ' (one after another — safest on small instances, the CLI is CPU/RAM hungry)': '（串行铸造——小实例上最稳妥，CLI 很吃 CPU/内存）',
  'Harvester unavailable': '收割机不可用',
  ' — no opencode CLI in this container (ZEN_HARVEST_BIN).': '：容器内没有 opencode CLI（ZEN_HARVEST_BIN）。',
  '.': '。',
  'fails with 403 — normally the harvester mints one on startup, on repeated 403s, and every few hours; use the buttons below to mint immediately (e.g. right after a fresh deploy with many keys).': ' 会以 403 失败 —— 正常情况下收割机会在启动时、反复 403 时和每隔几小时自动补铸；也可用下方按钮立即铸造（例如刚部署、key 很多时）。',
  'Mint missing sessions': '铸造缺失的会话',
  'Force mint / refresh all': '强制重铸全部',
  'opencode CLI not available in this container': '本容器内没有 OpenCode CLI',
  'Last minted': '上次铸造',
  'Context': '上下文',
  'Maximum context length': '最大上下文长度',
  'OpenCode stats': 'OpenCode 统计',
  'Requests': '请求数',
  'Input tokens': '输入 token',
  'Output tokens': '输出 token',
  'Compaction': '压缩',
  'Rate-limited': '限流次数',
  'Today': '今日',
  'Total': '累计',
  'Per-model breakdown (today)': '按模型拆分（今日）',
  'No data': '暂无数据',
  // 模型状态标签
  'error': '错误',
  // 通用 toast / 确认 / 片段
  'Saved ': '已保存 ',
  ' headers': ' 个请求头',
  ' proxies': ' 个代理',
  'Proxy pool saved': '代理池已保存',
  'Global policy saved': '全局策略已保存',
  'WorkBuddy accounts without proxy bindings follow this switch.': 'WorkBuddy 未绑定代理的账号遵循此开关。',
  'Proxy isolation settings saved': '代理隔离设置已保存',
  'Pool': '启用',
  'Participates in rotation': '参与轮换',
  'Routing participation. Newly added accounts start unchecked — tick Pool to join rotation.': '路由参与开关。新增账号默认不勾选，勾选 Pool 后才参与轮换。',
  'Routing participation: unchecked keys never enter request rotation, and minting follows routing (a routing-disabled key is no longer auto-minted). Newly added keys start disabled — tick Pool to enable.': '路由参与开关：不勾选的 key 不进入请求轮转，铸造跟随路由（路由禁用的 key 不再自动铸造）。新增 key 默认禁用，勾选 Pool 后启用。',
  'Participates in rotation. Newly added accounts start disabled — tick Pool to join rotation.': '参与轮换。新增账号默认禁用，勾选 Pool 后才参与轮换。',
  'Save failed: ': '保存失败：',
  'Test failed: ': '测试失败：',
  'Check failed: ': '检查失败：',
  'Delete failed: ': '删除失败：',
  'Refresh failed: ': '刷新失败：',
  'Export failed: ': '导出失败：',
  'Update failed: ': '更新失败：',
  'Generation failed: ': '生成失败：',
  'Add failed: ': '添加失败：',
  'Import failed: ': '导入失败：',
  'Create failed: ': '创建失败：',
  'Mint failed: ': '铸造失败：',
  'Failed to load accounts: ': '账号加载失败：',
  'Failed to load: ': '加载失败：',
  'Account deleted': '账号已删除',
  'All accounts deleted': '已删除全部账号',
  'All persisted data deleted': '已删除全部持久化数据',
  'All keys deleted': '已删除全部 key',
  'Key deleted': 'key 已删除',
  'Key generated': 'key 已生成',
  'New key generated (click to copy)': '新 key 已生成（点击复制）',
  'Copied to clipboard': '已复制到剪贴板',
  'Account added: ': '账号已添加：',
  'Account ': '账号 ',
  ' — ': ' —— ',
  'Available': '可用',
  'Error': '错误',
  '\nEstimated recovery: ': '\n预计恢复：',
  '\nReason: ': '\n原因：',
  '\nHTTP: ': '\nHTTP：',
  ' (estimated recovery ': '（预计恢复 ',
  ' (remaining ': '（剩余 ',
  ')': '）',
  'OK': '成功',
  ' — cooldown cleared': ' —— 冷却已清除',
  ' via ': '，模型 ',
  'Strategy updated to: ': '调度策略已更新为：',
  'Rows with a value but no key were ignored': '有值但没有键名的行已被忽略',
  'Headers saved': '请求头已保存',
  'Enter a token or API key': '请输入 token 或 API key',
  'Enter an alias ID': '请输入别名 ID',
  'Select a target model': '请选择目标模型',
  'Combo created: ': '自定义别名已创建：',
  'Combo deleted': '自定义别名已删除',
  'Delete combo ': '删除自定义别名 ',
  '? Clients using this alias will no longer be able to call it.': '？使用该别名的客户端将无法继续调用。',
  'Delete this account?': '确定删除该账号？',
  'Delete ALL accounts? This cannot be undone!': '确定删除全部账号？此操作不可撤销！',
  'Delete this key?': '确定删除该 key？',
  'Delete ALL API keys?': '确定删除全部 API key？',
  'OAuth failed: ': 'OAuth 登录失败：',
  'Testing': '测试中',
  'Checking': '检查中',
  'Force minting all sessions…': '正在强制重铸全部会话…',
  'Minting missing sessions…': '正在铸造缺失的会话…',
  'last mint ': '上次铸造 ',
  ' (force)': '（强制）',
  'Running — ': '进行中 —— ',
  ' minted, ': ' 成功, ',
  ' failed, ': ' 失败, ',
  ' pending': ' 待处理',
  'Done — ': '完成 —— ',
  ' already live': ' 已是 live',
  ' skipped (already live)': ' 跳过（已是 live）',
  'Recovers ': '恢复于 ',
  'No opencode keys configured': '没有配置任何 opencode key',
  'Failed to load': '加载失败',
  'Default model saved: ': '默认模型已保存：',
  'Invalid proxy format: ': '代理格式无效：',
  'No models': '没有模型',
  '· official feed: ': '· 官方源同步于 ',
  'Check complete': '检查完成',
  'cooldown': '冷却中',
  'active': '可用',
  'expired': '已失效'
};
function T(s) { return LANG === 'zh' ? (I18N_ZH[s] || s) : s; }
function toggleLang() {
  localStorage.setItem('lang', LANG === 'zh' ? 'en' : 'zh');
  location.reload();
}
// 文本节点翻译：保留首尾空白（HTML 缩进），整段替换
function i18nTextNode(n) {
  const raw = n.nodeValue;
  const s = raw.trim();
  if (!s) return;
  const t = I18N_ZH[s];
  if (!t) return;
  const lead = raw.slice(0, raw.length - raw.trimStart().length);
  const trail = raw.slice(raw.trimEnd().length);
  n.nodeValue = lead + t + trail;
}
function i18nAttrs(el) {
  if (!el.getAttribute) return;
  ['title', 'placeholder'].forEach(a => {
    const v = el.getAttribute(a);
    if (!v) return;
    const t = I18N_ZH[v.trim()];
    // Same-value setAttribute still emits an attribute mutation and can loop
    // forever when a translated placeholder/title maps to itself.
    if (t && t !== v) el.setAttribute(a, t);
  });
}
function i18nSubtree(root) {
  if (LANG !== 'zh') return;
  const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  while (w.nextNode()) i18nTextNode(w.currentNode);
  if (root.querySelectorAll) {
    root.querySelectorAll('[title],[placeholder]').forEach(i18nAttrs);
  }
  if (root.nodeType === 1) i18nAttrs(root);
}
// 观察动态插入（表格行/toast/状态标签等）：纯英文文本节点即时翻译。
// 翻译写入自身触发的新 mutation 会查中文键必 miss，自动收敛不会死循环。
const i18nMO = new MutationObserver(muts => {
  if (LANG !== 'zh') return;
  for (const m of muts) {
    if (m.type === 'attributes') { i18nAttrs(m.target); continue; }
    for (const n of m.addedNodes) {
      if (n.nodeType === 3) i18nTextNode(n);
      else if (n.nodeType === 1) i18nSubtree(n);
    }
  }
});
i18nMO.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['title', 'placeholder'] });
i18nSubtree(document.body);
if (LANG === 'zh') document.documentElement.lang = 'zh-CN';
(function initLangBtn() {
  const b = _('langBtn');
  if (b) b.textContent = LANG === 'zh' ? 'EN' : '中';
})();

if (window.location.host) {
  _('footerApiAddr').textContent = 'http://' + window.location.host;
}

function toast(msg, t, duration) {
  const el = _('toast');
  el.textContent = msg;
  el.style.whiteSpace = 'pre-line';
  el.className = 'toast ' + (t || 'info') + ' show';
  clearTimeout(el._hideTimer);
  try {
    if (el.matches(':popover-open')) el.hidePopover();
    el.showPopover();
  } catch (e) { /* older browsers fall back to z-index stacking */ }
  clearTimeout(el._timer);
  el._timer = setTimeout(() => {
    el.classList.remove('show');
    el._hideTimer = setTimeout(() => {
      try { if (el.matches(':popover-open')) el.hidePopover(); } catch (e) { /* ignore */ }
    }, 260);
  }, duration || 3500);
}
function showWorkbuddyDialog(dialog) {
  if (dialog && !dialog.open) dialog.showModal();
  // 重新打开通知可把它重新推到 top layer 顶部，避免先弹通知、后开弹窗时被遮住。
  const el = _('toast');
  try {
    if (el && el.matches(':popover-open')) {
      el.hidePopover();
      el.showPopover();
    }
  } catch (e) { /* ignore */ }
}

// ========== Navigation ==========
function ensureWorkbuddyConsole() {
  loadWorkbuddyAccounts();
  loadWorkbuddyConfig();
}
document.querySelectorAll('.nav-item').forEach(el => {
  el.addEventListener('click', () => {
    if (el.classList.contains('active')) return;
    switchTab(el.dataset.tab);
  });
});
// restoreTab 刷新/重开后恢复上次浏览的页：switchTab 把当前页写进 location.hash，
// 这里只在 hash 命中真实存在的 tab 时才恢复（tab-* 元素存在性即白名单）。
// 注意只能声明不能立即调用：switchTab 会触达各平台的 const（如 WB_API_PREFIX），
// 在脚本中段执行会撞 TDZ（"Cannot access before initialization"），调用点在末尾 Init 区。
function restoreTab() {
  const h = (location.hash || '').replace(/^#/, '');
  if (h && _('tab-' + h)) switchTab(h);
}

function switchTab(name) {
  document.querySelectorAll('.nav-item').forEach(e => {
    e.classList.toggle('active', e.dataset.tab === name);
  });
  document.querySelectorAll('.tab-panel').forEach(e => e.style.display = 'none');
  _('tab-' + name).style.display = 'block';
  if (name === 'dashboard') { loadStats(); loadAccounts(); }
  if (name === 'accounts') loadAccounts();
  if (name === 'settings') { loadKeys(); loadModels(); loadConfig(); }
  if (name === 'logs') loadLogs();
  if (name === 'proxypool') loadProxyPool();
  if (name === 'opencode') { loadOcConfig(); loadOcModels(); loadOcStats(); loadOcSessions(); }
  if (name === 'workbuddy') ensureWorkbuddyConsole();
  if (name === 'combos') { loadCombos(); fillComboModels(); }
  // 把当前页写进 hash：刷新/重开浏览器后 restoreTab 恢复到离开时的页
  try { history.replaceState(null, '', '#' + name); } catch (e) { /* ignore */ }
}

// ========== Integrated WorkBuddy console ==========
const WB_API_PREFIX = '/admin/workbuddy/api';
let wbAccounts = [];
let wbProxyCache = { bindings: {} };
let wbOAuthTimer = null;

async function wbCall(action, params, body) {
  const query = new URLSearchParams();
  Object.entries(params || {}).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') query.set(k, String(v));
  });
  const init = { method: body ? 'POST' : 'GET', headers: { 'Accept': 'application/json' } };
  if (body) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const qs = query.toString();
  const encodedAction = action.split('/').map(encodeURIComponent).join('/');
  const res = await fetch(WB_API_PREFIX + '/' + encodedAction + (qs ? '?' + qs : ''), init);
  if (res.status === 401) {
    location.reload();
    throw new Error('Admin session expired');
  }
  let data = {};
  try { data = await res.json(); } catch (e) { /* preserve HTTP error below */ }
  if (!res.ok) throw new Error(data.error || data.message || ('HTTP ' + res.status));
  if (data.ok === false || data.success === false) throw new Error(data.error || data.message || ('HTTP ' + res.status));
  if (data.error && !data.config && !data.accounts) throw new Error(data.error);
  return data.data !== undefined ? data.data : data;
}

function wbDig(obj, path) {
  return String(path || '').split('.').reduce((v, k) => (v == null ? undefined : v[k]), obj);
}
function wbPut(obj, path, value) {
  const keys = String(path || '').split('.').filter(Boolean);
  if (!keys.length) return;
  let cur = obj;
  keys.slice(0, -1).forEach(k => {
    if (!cur[k] || typeof cur[k] !== 'object') cur[k] = {};
    cur = cur[k];
  });
  cur[keys[keys.length - 1]] = value;
}
function wbRowId(account) {
  const value = account && (account.uid ?? account.account_id ?? account.id);
  return value === undefined || value === null ? '' : String(value);
}
function wbStatusInfo(account) {
  const status = String(account.status || (account.disabled ? 'disabled' : (account.cooling ? 'cooling' : 'active'))).toLowerCase();
  if (status === 'ready' || status === 'authenticated' || status === 'active' || status === 'enabled') {
    return { cls: 'active', label: status === 'authenticated' ? 'Authenticated' : (status === 'active' ? 'Active' : 'Ready') };
  }
  if (status === 'cooling' || status === 'cooldown' || status === 'suspended') {
    return { cls: 'cooldown', label: status === 'suspended' ? 'Suspended' : 'Cooling' };
  }
  if (status === 'disabled' || status === 'deleted' || status === 'error' || status === 'failed') {
    return { cls: 'expired', label: status === 'deleted' ? 'Deleted' : (status === 'disabled' ? 'Disabled' : 'Error') };
  }
  return { cls: 'expired', label: status === 'unknown' ? 'Unknown' : status };
}
function wbFmtTime(value) {
  if (!value || String(value).startsWith('0001-')) return '—';
  const date = new Date(value);
  return isNaN(date.getTime()) ? String(value) : date.toLocaleString('en-US');
}
function wbFmtTokens(value) {
  const n = Number(value || 0);
  if (n >= 1000000) return (n / 1000000).toFixed(2).replace(/\.?0+$/, '') + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'K';
  return String(n);
}
function wbBindingCell(account) {
  const id = wbRowId(account);
  const binding = (wbProxyCache.bindings || {})[id] || {};
  const main = String(binding.main ?? binding.main_proxy_id ?? '');
  const backup = String(binding.backup ?? binding.backup_proxy_id ?? '');
  // 辅代理开关关闭 → 辅槽只读跟随主槽（同主=全局/直连 时的联动）
  const backupLocked = !backupProxyOn || main === '' || main === EGRESS_DIRECT;
  const backupVal = backupLocked ? main : backup;
  const stale = v => v && v !== EGRESS_DIRECT && !proxyListCache.includes(v);
  const staleMark = (stale(main) || stale(backup))
    ? ' <span title="' + T('Bound egress no longer in the proxy pool — isolation logic skips this account') + '" style="color:var(--danger)">⚠</span>'
    : '';
  return '<td style="white-space:nowrap">' +
    egComboHTML('wb', id, 'main', main, T('Main egress: requests go through this egress whenever available')) + ' ' +
    egComboHTML('wb', id, 'backup', backupVal, T('Backup egress: fallback while the main egress is cooling down or unavailable'), backupLocked) +
    staleMark +
    '</td>';
}
function wbActionButton(action, id, label, extra) {
  return '<button type="button" class="btn btn-sm ' + (extra || '') + '" data-wb-action="' + esc(action) + '" data-wb-id="' + esc(id) + '" onclick="workbuddyActionFrom(this)">' + esc(label) + '</button>';
}
function renderWorkbuddyAccounts() {
  const tbody = _('wbAccountsBody');
  if (!tbody) return;
  if (!wbAccounts.length) {
    tbody.innerHTML = '<tr><td colspan="9" class="empty">No accounts yet</td></tr>';
    return;
  }
  wbSnapshot = {};
  tbody.innerHTML = wbAccounts.map(account => {
    const id = wbRowId(account);
    const status = wbStatusInfo(account);
    const realm = String(account.realm || 'cn');
    const usage = account.token_usage || {};
    const runs = usage.request_count ?? 0;
    const errors = account.err_total ?? 0;
    const success = account.success_count ?? 0;
    const tokens = usage.total_tokens ?? 0;
    const credits = account.credits ?? account.remaining_credits ?? '—';
    const creditsTotal = account.credits_total ?? account.total_credits;
    const last = account.last_success;
    const reason = account.disabled_reason || account.reason || '';
    const statusHtml = '<span class="status ' + status.cls + '"><span class="status-dot ' + status.cls + '"></span>' + esc(status.label) + '</span>' +
      (reason ? '<div class="hint" style="font-size:11px;margin-top:3px">' + esc(reason) + '</div>' : '');
    const usageHtml = '<div class="wb-usage">' + esc(wbFmtTokens(tokens)) + ' tok</div>' +
      '<div class="hint" style="font-size:11px;margin-top:2px">' + esc(runs) + ' requests</div>';
    // Pool（启用）列：勾选 = 进入账号池参与选号；未设置（pool_enabled 缺失）
    // 按启用渲染。勾选改动先暂存，由"保存"按钮统一提交。
    const poolEnabled = account.pool_enabled !== false;
    wbSnapshot[id] = { enabled: poolEnabled };
    const poolCell = '<td style="text-align:center"><input type="checkbox" data-wb-pool="' + esc(id) + '"' +
      (poolEnabled ? ' checked' : '') + ' title="' + esc(T('Participates in rotation')) + '" aria-label="' + esc(T('Participates in rotation')) + '"></td>';
    // uid 过长会把 Account 列撑宽，昵称下方只留前 10 个字符，完整值走 title 悬浮。
    const shortId = id.length > 10 ? id.slice(0, 10) + '…' : id;
    return '<tr draggable="true" data-wb-id="' + esc(id) + '" data-drag-id="' + esc(id) + '">' +
      '<td><div class="wb-account"><div class="wb-account-main">' +
        '<div class="wb-account-name">' + esc(account.name || account.email || account.nickname || ('Account ' + id)) + '</div>' +
        '<div class="wb-account-id" title="' + esc(realm + ' · ' + id) + '">' + esc(realm) + ' · ' + esc(shortId) + '</div>' +
      '</div></div></td>' +
      '<td>' + statusHtml + '</td>' +
      '<td>' + esc(credits) + (creditsTotal ? '<span style="color:var(--text3)"> / ' + esc(creditsTotal) + '</span>' : '') + '</td>' +
      '<td>' + usageHtml + '</td>' +
      '<td class="wb-usage">' + esc(success) + ' / ' + esc(errors) + '</td>' +
      '<td style="font-size:11px;color:var(--text3)">' + esc(wbFmtTime(last)) + '</td>' +
      wbBindingCell(account) +
      '<td><div class="wb-action-group">' +
        wbActionButton('refresh', id, 'Refresh') +
        wbActionButton('checkin', id, 'Check-in') +
        wbActionButton('details', id, 'Points') +
        wbActionButton('delete', id, 'Delete', 'btn-danger') +
      '</div></td>' +
      poolCell +
      '</tr>';
  }).join('');
  tbody.onchange = e => {
    const c = e.target.closest('input[data-wb-pool]');
    if (c) markRowDirty(c.closest('tr'), 'wb');
  };
}
async function loadWorkbuddyProxyState() {
  try {
    const state = await workbuddyProxyAdminCall('GET', 'proxy');
    wbProxyCache = { bindings: state.bindings || {} };
  } catch (e) { /* account list still renders without the optional proxy list */ }
  return wbProxyCache;
}
function updateWorkbuddySummary() {
  const summary = _('wbSummary');
  if (!summary) return;
  const active = wbAccounts.filter(a => wbStatusInfo(a).cls === 'active').length;
  const cooling = wbAccounts.filter(a => wbStatusInfo(a).cls === 'cooldown').length;
  summary.textContent = wbAccounts.length + ' ' + T('accounts') + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
}
async function loadWorkbuddyAccounts() {
  const tbody = _('wbAccountsBody');
  if (tbody) tbody.innerHTML = '<tr><td colspan="9" class="empty">Loading...</td></tr>';
  try {
    await loadProxyListCache();
    const [overview] = await Promise.all([wbCall('overview'), loadWorkbuddyProxyState()]);
    wbAccounts = overview.accounts || [];
    renderWorkbuddyAccounts();
    updateWorkbuddySummary();
  } catch (e) {
    // WorkBuddy 子系统启动失败被禁用时 overview 路由不存在（404）：
    // 与"暂无账号"同态展示，不把装配失败透传成硬错误。
    if (String(e && e.message || '').indexOf('404') >= 0) {
      wbAccounts = [];
      renderWorkbuddyAccounts();
    } else if (tbody) {
      tbody.innerHTML = '<tr><td colspan="8" class="empty">' + esc(T('Failed to load WorkBuddy accounts: ') + e.message) + '</td></tr>';
    }
    updateWorkbuddySummary();
  }
}
function workbuddyBindingId(value) {
  return String(value ?? '').trim();
}
async function workbuddyProxyAdminCall(method, path, body) {
  const response = await api(method, '/workbuddy/' + path, body);
  return response.data !== undefined ? response.data : response;
}
// wbSnapshot 记录各账号已保存的启用状态（代理绑定在 wbProxyCache 里），
// "保存"按钮据此 diff 出真正需要提交的账号。
let wbSnapshot = {};
async function saveWorkbuddyEdits() {
  const rows = [...document.querySelectorAll('#wbAccountsBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="wb"][data-slot="main"]');
    if (!mainInput) continue;
    const uid = mainInput.dataset.egid;
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="wb"][data-slot="backup"]').dataset.value || '';
    const cb = row.querySelector('input[data-wb-pool]');
    const binding = (wbProxyCache.bindings || {})[uid] || {};
    const snapMain = String(binding.main ?? binding.main_proxy_id ?? '');
    const snapBackup = String(binding.backup ?? binding.backup_proxy_id ?? '');
    const snapEnabled = wbSnapshot[uid] ? wbSnapshot[uid].enabled : true;
    try {
      if (main !== snapMain || backup !== snapBackup) {
        await workbuddyProxyAdminCall('POST', 'proxy/set', { uid: uid, main: main, backup: backup });
      }
      if (cb && cb.checked !== snapEnabled) {
        await api('POST', '/workbuddy/enabled', { uid: uid, enabled: cb.checked });
      }
      if (main !== snapMain || backup !== snapBackup || (cb && cb.checked !== snapEnabled)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('wbAccountsBody', 'wbSaveBtn');
  if (failed) toast(T('Failed to save ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account edits'), 'success');
  await loadWorkbuddyAccounts();
}
function stageAllWbPool(enabled) {
  document.querySelectorAll('#wbAccountsBody input[data-wb-pool]').forEach(cb => {
    const snap = wbSnapshot[cb.dataset.wbPool];
    if (snap && snap.enabled === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'wb');
  });
}
function stageClearWorkbuddyProxyBindings() {
  if (!confirm(T('Clear proxy bindings on ALL accounts and disable them? Takes effect when you click Save.'))) return;
  document.querySelectorAll('#wbAccountsBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="wb"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'wb');
    const cb = row.querySelector('input[data-wb-pool]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'wb');
  });
}
// 复位：丢弃账号表的全部暂存修改，按上次保存的状态重绘
function resetWorkbuddyEdits() {
  clearDirtyMarks('wbAccountsBody', 'wbSaveBtn');
  loadWorkbuddyAccounts();
}
async function runWorkbuddyBatch(action) {
  const bodies = {
    checkin_all: {},
    travel_all: { accept_and_complete: true, force_complete: true, chain_cycles: 1 },
    activity_all: {},
    keepalive_all: {},
    balance_all: {}
  };
  try {
    const result = await wbCall(action, {}, bodies[action] || {});
    toast(result.message || (action + ' started'), 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast(action + ' failed: ' + e.message, 'error'); }
}
async function workbuddyActionFrom(button) {
  const action = button.dataset.wbAction;
  const id = workbuddyBindingId(button.dataset.wbId);
  if (action === 'details') { openWorkbuddyDetails(id, button.dataset.wbId); return; }
  if (action === 'delete' && !confirm(T('Delete this WorkBuddy account and its credentials?'))) return;
  const accountPath = 'accounts/' + encodeURIComponent(id);
  const actions = {
    refresh: accountPath + '/balance',
    checkin: accountPath + '/checkin',
    delete: accountPath + '/remove'
  };
  if (!actions[action]) return;
  button.disabled = true;
  try {
    const result = await wbCall(actions[action], {}, {});
    toast(result.message || (action + ' completed'), 'success');
    await Promise.all([loadWorkbuddyAccounts(), loadWorkbuddyConfig()]);
  } catch (e) { toast(action + ' failed: ' + e.message, 'error'); }
  finally { button.disabled = false; }
}
async function refreshWorkbuddyAccounts() {
  try {
    const result = await wbCall('balance_all', {}, {});
    toast(result.message || 'Balances refreshed', 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast('Refresh failed: ' + e.message, 'error'); }
}

const WB_PK_COLORS = ['#4f8cff', '#25b08b', '#e8a33d', '#c96bd6', '#e2607a',
                      '#5aa9e6', '#8fbf3f', '#b58b5a', '#7d8fa8', '#d4785c'];
function wbPackageSources(packs) {
  const out = new Map();
  (packs || []).forEach(p => {
    const key = String(p.package_code || '') + '|' + String(p.name || T('(unnamed)'));
    const item = out.get(key) || {
      key: key, name: p.name || T('(unnamed)'), count: 0, remain: 0, size: 0, used: 0,
      minCreated: '', minEnd: ''
    };
    item.count += 1;
    item.remain += Number(p.remain || 0);
    item.size += Number(p.size || 0);
    item.used += Number(p.used || 0);
    const created = String(p.created_at || '').slice(0, 10);
    if (created && (!item.minCreated || created < item.minCreated)) item.minCreated = created;
    const end = String(p.end_time || '').slice(0, 10);
    if (end && (!item.minEnd || end < item.minEnd)) item.minEnd = end;
    out.set(key, item);
  });
  return Array.from(out.values()).sort((a, b) => b.size - a.size);
}
function wbPackageLabel(name) {
  return String(name || '').replace(/^CodeBuddy/, '');
}
let wbDetailsAccountId = null;
function openWorkbuddyDetails(id, label) {
  wbDetailsAccountId = workbuddyBindingId(id);
  _('wbDetailsWho').textContent = label || id;
  _('wbDetailsStatus').textContent = T('Querying points breakdown from upstream...');
  _('wbDetailsBody').innerHTML = '';
  showWorkbuddyDialog(_('wbDetailsDialog'));
  loadWorkbuddyDetails();
}
function closeWorkbuddyDetails() {
  wbDetailsAccountId = null;
  const dialog = _('wbDetailsDialog');
  if (dialog && dialog.open) dialog.close();
}
async function loadWorkbuddyDetails() {
  if (wbDetailsAccountId == null) return;
  try {
    const data = await wbCall('packages');
    const account = (data.accounts || []).find(a => String(a.uid) === String(wbDetailsAccountId));
    if (!account) throw new Error(T('No points data found for this account'));
    renderWorkbuddyDetails(account);
    _('wbDetailsStatus').textContent = T('Live from upstream · ') + (account.packages || []).length + T(' points packages');
  } catch (e) {
    _('wbDetailsStatus').textContent = T('Failed to read: ') + e.message;
    _('wbDetailsBody').innerHTML = '';
  }
}
function renderWorkbuddyDetails(a) {
  const body = _('wbDetailsBody');
  if (a.error) {
    body.innerHTML = '<div class="empty">' + T('Query failed: ') + esc(a.error) + '</div>';
    return;
  }
  const packs = a.packages || [];
  const sources = wbPackageSources(packs);
  const total = Math.max(1, Number(a.size || 0));
  const colorOf = key => WB_PK_COLORS[Math.max(0, sources.findIndex(s => s.key === key)) % WB_PK_COLORS.length];
  const mix = sources.map(s =>
    '<i style="width:' + (s.size / total * 100).toFixed(2) + '%;background:' + colorOf(s.key) + '" title="' +
    esc(wbPackageLabel(s.name)) + ' ' + esc(wbFmtTokens(s.size)) + '"></i>').join('');
  const legend = sources.map(s =>
    '<span><i style="background:' + colorOf(s.key) + '"></i>' + esc(wbPackageLabel(s.name)) +
    ' x' + s.count + ' · ' + esc(wbFmtTokens(s.size)) +
    (s.minCreated ? ' · ' + T('first issued ') + esc(s.minCreated.slice(5)) : '') + '</span>').join('');
  const rows = packs.map(p => {
    const key = String(p.package_code || '') + '|' + String(p.name || T('(unnamed)'));
    const sub = String(p.sub_product_code || '').replace(/^sp_tcaca_codebuddyide_?/, '') ||
      String(p.package_code || '').replace(/^TCACA_/, '');
    return '<tr><td><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:' +
      colorOf(key) + '"></span></td>' +
      '<td>' + esc(p.name || T('(unnamed)')) + (sub ? '<div class="hint" style="font-size:11px">' + esc(sub) + '</div>' : '') + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.size)) + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.remain)) + '</td>' +
      '<td class="text-right">' + esc(wbFmtTokens(p.used)) + '</td>' +
      '<td class="text-right">' + esc(String(p.created_at || '').slice(0, 16).replace('T', ' ') || '—') + '</td>' +
      '<td class="text-right">' + esc(String(p.end_time || '').slice(0, 10) || '—') + '</td></tr>';
  }).join('');
  body.innerHTML =
    '<div class="wb-pk-card"><div class="wb-pk-head"><div><div class="wb-pk-name">' +
      esc(a.nickname || a.uid) + '</div><div class="wb-pk-sub">' + esc(a.realm || '') + ' · ' + esc(a.uid) + '</div></div>' +
      '<div class="wb-pk-sub">' + T('Total ') + esc(wbFmtTokens(a.size)) + ' · ' + packs.length + T(' package(s)') + '</div></div>' +
      '<div class="wb-pk-big">' + esc(wbFmtTokens(a.remain)) + '</div>' +
      '<div class="wb-pk-sub">' + T('Remaining points · used ') + esc(wbFmtTokens(Math.max(0, Number(a.size || 0) - Number(a.remain || 0)))) + '</div>' +
      '<div class="wb-pk-mix">' + mix + '</div><div class="wb-pk-legend">' + legend + '</div></div>' +
    '<div class="table-wrap"><table><thead><tr><th></th><th>' + T('Package / source') + '</th><th class="text-right">' + T('Face value') + '</th>' +
      '<th class="text-right">' + T('Remaining') + '</th><th class="text-right">' + T('Used') + '</th><th class="text-right">' + T('Granted') + '</th>' +
      '<th class="text-right">' + T('Expires') + '</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="7" class="empty">' + T('No points packages') + '</td></tr>') + '</tbody></table></div>';
}

let wbTodoTimer = null;
let wbTodoSeq = 0;
function wbTodoConcurrency() {
  const value = Number(_('wbTodoConcurrency') && _('wbTodoConcurrency').value);
  return Math.min(3, Math.max(1, Number.isFinite(value) ? value : 1));
}
function openWorkbuddyTodoScan() {
  wbTodoSeq = 0;
  _('wbTodoStatus').textContent = T('Pick a concurrency, then click "Scan tasks"');
  _('wbTodoBody').innerHTML = '';
  showWorkbuddyDialog(_('wbTodoDialog'));
}
function closeWorkbuddyTodoScan() {
  if (wbTodoTimer) { clearTimeout(wbTodoTimer); wbTodoTimer = null; }
  wbTodoSeq = 0;
  const dialog = _('wbTodoDialog');
  if (dialog && dialog.open) dialog.close();
}
function renderWorkbuddyTodoAccounts(data) {
  const accounts = data.accounts || [];
  const groups = accounts.filter(a => (a.growth || []).length || a.growth_error);
  if (!groups.length) {
    _('wbTodoBody').innerHTML = '<div class="empty">' + T('No pending tasks for all accounts') + '</div>';
    return;
  }
  _('wbTodoBody').innerHTML = groups.map(a => {
    const rows = (a.growth || []).map(t => {
      const progress = t.target ? (t.current || 0) + ' / ' + t.target : '—';
      const title = t.title || t.task_code || '';
      const desc = [t.task_desc, t.description].filter(Boolean).join('\n');
      const reward = [
        Number(t.credit || 0) > 0 ? T('Points') + ' ' + t.credit : '',
        Number(t.energy || 0) > 0 ? T('Energy') + ' ' + t.energy : ''
      ].filter(Boolean).join(' · ');
      const tip = [
        T('Task: ') + title,
        T('Description: ') + (desc || '—'),
        T('Progress: ') + progress,
        T('Reward: ') + (reward || '—')
      ].join('\n');
      return '<div class="wb-todo-row" title="' + esc(tip) + '"><div class="nm"><div>' + esc(title) +
        '</div><div class="id">' + esc(t.task_code || '') + '</div></div><span class="model-tag">' + T('Pending') + '</span>' +
        '<span class="wb-usage">' + esc(progress) + '</span></div>';
    }).join('');
    const err = a.growth_error ? '<div class="hint" style="color:var(--danger)">' + T('Scan failed: ') + '' + esc(a.growth_error) + '</div>' : '';
    return '<details class="wb-todo-group"><summary class="wb-todo-title">' + esc(a.nickname || a.uid) +
      ' <span class="auto-pill">' + esc(a.uid || '') + ' · ' + T(' item(s)') + '</span></summary>' + rows + err + '</details>';
  }).join('');
}
async function scanWorkbuddyTodos() {
  const concurrency = wbTodoConcurrency();
  const button = _('wbTodoRunAll');
  const scanButton = _('wbTodoScan');
  if (button) button.disabled = true;
  if (scanButton) scanButton.disabled = true;
  _('wbTodoStatus').textContent = T('Scanning with concurrency ') + concurrency + T(' scanning tasks...');
  _('wbTodoBody').innerHTML = '<div class="empty">' + T('Scanning...') + '</div>';
  try {
    const data = await wbCall('tasks/scan_all', {}, { concurrency: concurrency });
    renderWorkbuddyTodoAccounts(data);
    _('wbTodoStatus').textContent = T('Scan complete: ') + (data.pending_count || 0) + T(' pending item(s)');
  } catch (e) {
    _('wbTodoStatus').textContent = T('Scan failed: ') + e.message;
    _('wbTodoBody').innerHTML = '';
  } finally {
    if (button) button.disabled = false;
    if (scanButton) scanButton.disabled = false;
  }
}
function renderWorkbuddyQueue(data) {
  const items = data.items || [];
  if (!items.length) return;
  const groups = new Map();
  items.forEach(it => {
    const key = String(it.uid || '');
    if (!groups.has(key)) groups.set(key, { uid: key, nickname: it.nickname || key, items: [] });
    groups.get(key).items.push(it);
  });
  const labels = { pending: T('Claimed'), running: T('In progress'), done: T('Done'), skipped: T('Skipped'), error: T('Failed') };
  _('wbTodoBody').innerHTML = Array.from(groups.values()).map(g =>
    '<details class="wb-todo-group"><summary class="wb-todo-title">' + esc(g.nickname) +
      ' <span class="auto-pill">' + esc(g.uid) + ' · ' + T(' item(s)') + '</span></summary>' +
      g.items.map(it => {
        const title = it.title || it.code || '';
        const tip = [
          T('Task: ') + title,
          T('Description: ') + (it.desc || '—'),
          it.progress ? T('Progress: ') + it.progress : '',
          it.message ? T('Result: ') + it.message : ''
        ].filter(Boolean).join('\n');
        const sub = [it.code, it.progress, it.message].filter(Boolean).join(' · ');
        return '<div class="wb-todo-row" title="' + esc(tip) + '"><div class="nm"><div>' + esc(title) +
          '</div><div class="id">' + esc(sub) + '</div></div><span class="model-tag">' +
          esc(labels[it.status] || it.status || '') + '</span></div>';
      }).join('') + '</details>').join('');
}
async function pollWorkbuddyQueue() {
  const seq = wbTodoSeq;
  if (!seq) return;
  try {
    const data = await wbCall('tasks/queue');
    if (seq !== wbTodoSeq) return;
    renderWorkbuddyQueue(data);
    const done = (data.items || []).filter(it => ['done', 'skipped', 'error'].includes(it.status)).length;
    _('wbTodoStatus').textContent = (data.running ? T('Running') : T('Finished')) + T(': ') + done + ' / ' + (data.total || 0);
    if (data.running) {
      wbTodoTimer = setTimeout(pollWorkbuddyQueue, 1500);
    } else {
      wbTodoTimer = null;
      await loadWorkbuddyAccounts();
    }
  } catch (e) {
    _('wbTodoStatus').textContent = T('Failed to read queue: ') + e.message;
  }
}
async function runWorkbuddyAllTodos() {
  const concurrency = wbTodoConcurrency();
  if (!confirm(T('Run all listed tasks with concurrency ') + concurrency + T(' (tasks run serially within each account, auto-claim enabled). Confirm?'))) return;
  const button = _('wbTodoRunAll');
  const scanButton = _('wbTodoScan');
  if (button) button.disabled = true;
  if (scanButton) scanButton.disabled = true;
  try {
    const result = await wbCall('tasks/run_queue', {}, { concurrency: concurrency, growth: true });
    if (!result.started) {
      toast(result.message || T('No pending tasks'), 'info');
      return;
    }
    wbTodoSeq = result.seq || 0;
    toast(T('Queue started: ') + result.total + T(' item(s) (concurrency ') + concurrency + T(')'), 'success');
    _('wbTodoStatus').textContent = T('Queue started, running...');
    wbTodoTimer = setTimeout(pollWorkbuddyQueue, 800);
  } catch (e) {
    toast(T('Failed to run: ') + e.message, 'error');
  } finally {
    if (button) button.disabled = false;
    if (scanButton) scanButton.disabled = false;
  }
}

function openWorkbuddyAdd() {
  const dialog = _('wbAddDialog');
  showWorkbuddyDialog(dialog);
  _('wbAddStatus').textContent = 'Choose a realm and request an authorization link.';
  _('wbOAuthUrl').value = '';
  _('wbStartLogin').disabled = false;
}
function closeWorkbuddyAdd() {
  if (wbOAuthTimer) { clearTimeout(wbOAuthTimer); wbOAuthTimer = null; }
  const dialog = _('wbAddDialog');
  if (dialog && dialog.open) dialog.close();
}
async function startWorkbuddyOAuth() {
  const button = _('wbStartLogin');
  button.disabled = true;
  button.textContent = 'Requesting...';
  _('wbAddStatus').textContent = 'Requesting authorization link...';
  try {
    const payload = await wbCall('login/start', { realm: _('wbRealm').value }, {});
    const url = payload.url || '';
    _('wbOAuthUrl').value = url;
    _('wbAddStatus').textContent = url ? 'Open the link and complete the browser login.' : 'Authorization session created.';
    if (url) wbOAuthTimer = setTimeout(() => pollWorkbuddyOAuth(payload.state), 1200);
  } catch (e) {
    _('wbAddStatus').textContent = T('OAuth failed: ') + e.message;
  } finally {
    button.disabled = false;
    button.textContent = 'Get authorization link';
  }
}
async function pollWorkbuddyOAuth(oauthId) {
  if (!oauthId) return;
  try {
    const payload = await wbCall('login/poll', { state: oauthId });
    if (payload.done) {
      _('wbAddStatus').textContent = 'Account added: ' + (payload.nickname || payload.uid || '');
      toast('WorkBuddy account added', 'success');
      await loadWorkbuddyAccounts();
      return;
    }
    if (payload.status === 'failed' || payload.error) throw new Error(payload.error || payload.status || 'failed');
    wbOAuthTimer = setTimeout(() => pollWorkbuddyOAuth(oauthId), 1500);
  } catch (e) { _('wbAddStatus').textContent = T('OAuth failed: ') + e.message; }
}
function copyWorkbuddyOAuthUrl() {
  const value = _('wbOAuthUrl').value;
  if (!value) return;
  copyText(value, 'Authorization link copied');
}
function openWorkbuddyOAuthUrl() {
  const value = _('wbOAuthUrl').value;
  if (value) window.open(value, '_blank', 'noopener');
}
async function importWorkbuddyAccounts(input) {
  const file = input.files && input.files[0];
  if (!file) return;
  try {
    const form = new FormData();
    form.append('file', file, file.name);
    const response = await fetch(API + '/workbuddy/accounts/import', { method: 'POST', body: form });
    const result = await response.json().catch(() => ({}));
    if (!response.ok || result.ok === false) throw new Error(result.error || response.statusText || 'upload failed');
    toast('WorkBuddy account imported', 'success');
    await loadWorkbuddyAccounts();
  } catch (e) { toast('Import failed: ' + e.message, 'error'); }
  input.value = '';
}

async function loadWorkbuddyConfig() {
  const form = _('wbConfigForm');
  if (!form) return;
  try {
    const result = await wbCall('config');
    const config = result.config || result;
    form.querySelectorAll('[data-wb-path]').forEach(el => {
      const value = wbDig(config, el.dataset.wbPath);
      if (el.type === 'checkbox') el.checked = !!value;
      else if (value === undefined || value === null) el.value = '';
      else if (Array.isArray(value)) el.value = value.join(', ');
      else el.value = String(value);
    });
    // 倍率筛选同样来自同一份配置：网关设置页的下拉与 WB 配置页共用一份真值
    // （saveWorkbuddyConfig 之后这里把新阈值同步进内存）。
    syncWorkbuddyRateFilter(config);
    _('wbConfigNote').textContent = T('loaded');
  } catch (e) {
    _('wbConfigNote').textContent = T('load failed');
    toast(T('WorkBuddy config load failed: ') + e.message, 'error');
  }
}
// syncWorkbuddyRateFilter 从服务端配置同步倍率筛选阈值；config 省略时自行拉取。
// 非法/缺失值回落默认 0.2（与 wbconfig.Default()、and 服务端兜底同一数值）。
async function syncWorkbuddyRateFilter(config) {
  let cfg = config;
  if (!cfg) {
    try {
      const result = await wbCall('config');
      cfg = result.config || result;
    } catch (e) { return; }
  }
  const raw = Number(wbDig(cfg, 'pool.model_rate_filter'));
  const next = WB_RATE_OPTIONS.includes(raw) ? raw : 0.2;
  if (next !== workbuddyRateFilter) {
    workbuddyRateFilter = next;
    if (_('wbRateFilter')) _('wbRateFilter').value = String(next);
  }
  return next;
}
function collectWorkbuddyConfig() {
  const out = {};
  _('wbConfigForm').querySelectorAll('[data-wb-path]').forEach(el => {
    const path = el.dataset.wbPath;
    const type = el.dataset.wbType || 'string';
    if (el.type === 'checkbox') { wbPut(out, path, el.checked); return; }
    const raw = (el.value || '').trim();
    if (!raw && !el.dataset.wbSendEmpty) return;
    if (type === 'ints') {
      const values = raw.split(/[\s,，]+/).map(Number).filter(Number.isFinite);
      if (values.length) wbPut(out, path, values);
      return;
    }
    if (type === 'number') {
      if (raw === '') return;
      const value = Number(raw);
      if (Number.isFinite(value)) wbPut(out, path, value);
      return;
    }
    wbPut(out, path, raw);
  });
  return out;
}
async function saveWorkbuddyConfig() {
  const button = _('wbSaveConfig');
  button.disabled = true;
  button.textContent = T('Saving...');
  try {
    const result = await wbCall('config', {}, collectWorkbuddyConfig());
    const restart = Array.isArray(result.restart_required) ? result.restart_required.length : 0;
    toast(restart ? T('WorkBuddy config saved (') + restart + T(' restart items)') : T('WorkBuddy config saved'), 'success');
    await loadWorkbuddyConfig();
  } catch (e) { toast(T('WorkBuddy config save failed: ') + e.message, 'error'); }
  finally { button.disabled = false; button.textContent = T('Save configuration'); }
}

(_('wbStartLogin') || {}).onclick = startWorkbuddyOAuth;
(_('wbConfigForm') || {}).addEventListener('submit', ev => { ev.preventDefault(); saveWorkbuddyConfig(); });

// ========== API helper ==========
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const res = await fetch(API + path, opts);
  if (res.status === 401 && path !== '/login') { location.reload(); return new Promise(() => {}); }
  const data = await res.json();
  if (!data.success && data.error) throw new Error(data.error);
  return data;
}

// ========== Dashboard ==========
// setDash 平台概览卡片：统一"N 账号 · N 可用 · N 冷却中"行文（OpenCode 单位为 key）
function setDash(id, unit, total, active, cooling) {
  const el = _(id);
  if (!el) return;
  el.textContent = total + ' ' + T(unit) + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
}
// setReq 请求统计块：统一"N 请求 · N 错误 · 平均 N ms"
function setReq(id, s) {
  const el = _(id);
  if (!el) return;
  s = s || {};
  el.textContent = (s.requests || 0) + ' ' + T('requests') + ' · ' + (s.errors || 0) + ' ' + T('errors') + ' · ' + T('Avg ') + Math.round(s.avg_ms || 0) + 'ms';
}
// loadStats 仪表盘三平台概览：Cline 走 /stats（含 expired 但概览行不展示），
// OpenCode 取 /opencode/config 的 keyStates（可用=路由启用），WorkBuddy 取
// 子系统 overview 的池计数（可用=healthy）。子系统被禁用时该卡片保持 "-"。
async function loadStats() {
  try {
    const d = await api('GET', '/stats');
    const s = d.data;
    setDash('dashCline', 'accounts', s.total, s.active, s.cooldown);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/opencode/config');
    const ks = (d.data && d.data.keyStates) || [];
    setDash('dashOc', 'keys', ks.length, ks.filter(k => k.routingEnabled !== false).length, ks.filter(k => k.cooling).length);
  } catch (e) { /* ignore */ }
  try {
    const ov = await wbCall('overview');
    setDash('dashWb', 'accounts', ov.total, ov.healthy, ov.cooling);
  } catch (e) { /* ignore */ }
  try {
    const d = await api('GET', '/request-stats');
    const rs = (d.data && d.data.routes) || {};
    setReq('reqCline', rs.cline);
    setReq('reqZen', rs.zen);
    setReq('reqWb', rs.workbuddy);
    const hint = _('reqStatsHint');
    if (hint && d.data && d.data.since) hint.textContent = T('Counted since ') + new Date(d.data.since).toLocaleString();
  } catch (e) { /* ignore */ }
}

// ========== Accounts ==========
// ========== 账号列表拖拽排序（Account pool 三菜单共用） ==========
// enableRowDragReorder 给账号表的 tbody 装上 HTML5 原生行拖拽：
// - dragover 实时把拖拽行插到目标位置（即时生效的视觉反馈，无需重建 DOM）；
// - 松手后按 tr[data-drag-id] 收集新顺序提交 submit(ids)，成功 toast 后 reload()
//   （reload 从后端重拉，保证 index/轮转游标等派生状态与磁盘一致），失败 reload 回滚；
// - 行内有未保存的暂存修改（.tr-dirty）时禁止起拖：OpenCode key 表的暂存协议
//   按 index 定位，重排会让未提交的 index 指向错误的 key。
// 拖拽从单元格空白/文本处起拖；input/select/button/label/a 上不起拖，避免
// 干扰下拉、勾选与按钮点击。空态行（无 data-drag-id）不参与。
function enableRowDragReorder(tbodyId, submit, reload) {
  const tb = _(tbodyId);
  if (!tb) return;
  tb.addEventListener('dragstart', e => {
    const tr = e.target.closest('tr[data-drag-id]');
    if (!tr || e.target.closest('input,select,textarea,button,a,label')) { e.preventDefault(); return; }
    if (tb.querySelector('tr.tr-dirty')) {
      e.preventDefault();
      toast(T('Save or discard staged edits before reordering'), 'warning');
      return;
    }
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', tr.dataset.dragId);
    tr.classList.add('row-dragging');
  });
  tb.addEventListener('dragover', e => {
    const dragging = tb.querySelector('tr.row-dragging');
    if (!dragging) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    const over = e.target.closest('tr[data-drag-id]');
    if (!over || over === dragging) return;
    const rect = over.getBoundingClientRect();
    const before = (e.clientY - rect.top) < rect.height / 2;
    tb.insertBefore(dragging, before ? over : over.nextSibling);
  });
  tb.addEventListener('drop', e => e.preventDefault());
  tb.addEventListener('dragend', async () => {
    const dragging = tb.querySelector('tr.row-dragging');
    if (!dragging) return;
    dragging.classList.remove('row-dragging');
    const ids = [...tb.querySelectorAll('tr[data-drag-id]')].map(tr => tr.dataset.dragId);
    try {
      await submit(ids);
      toast(T('Order saved'), 'success');
    } catch (err) {
      toast(T('Reorder failed: ') + err.message, 'error');
    }
    if (reload) reload(); // 成功=对齐派生状态；失败=回滚
  });
}

async function loadAccounts() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/accounts');
    const list = d.data.accounts;
    const tbody = _('accountTableBody');
    // 标题下统计：账号 · 可用 · 冷却中（与 WorkBuddy 页摘要同款）
    const summary = _('accSummary');
    if (summary) {
      const active = (list || []).filter(a => a.status === 'active').length;
      const cooling = (list || []).filter(a => a.status === 'cooldown').length;
      summary.textContent = (list || []).length + ' ' + T('accounts') + ' · ' + active + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    if (!list || list.length === 0) {
      tbody.innerHTML = '<tr><td colspan="8" class="empty">No accounts yet</td></tr>';
      return;
    }
    const sn = { active: 'Active', cooldown: 'Cooldown', expired: 'Expired' };
    accSnapshot = {};
    tbody.innerHTML = list.map(a => {
      const lu = a.lastUsed ? new Date(a.lastUsed).toLocaleString('en-US') : '-';
      const cr = a.createdAt ? new Date(a.createdAt).toLocaleString('en-US') : '-';
      // Cooldown label: show estimated recovery time
      let statusExtra = '';
      if (a.status === 'cooldown') {
        const until = a.cooldownUntil ? new Date(a.cooldownUntil).toLocaleString('en-US') : '';
        statusExtra = until ? '<div style="font-size:10px;color:var(--text3);margin-top:2px">' + T('Recovers ') + esc(until) + '</div>' : '';
      }
      // Proxy binding cells: main/backup comboboxes (Global / Direct / proxies
      // with type-ahead) saved on change. A binding that no longer exists in
      // the pool gets a stale marker — isolation keeps skipping the account
      // until it is fixed. The direct sentinel is never stale.
      // omitempty 序列化下未绑定账号没有 proxyMain/proxyBackup 键，归一化为 ''。
      const pMain = a.proxyMain || '';
      const pBackup = a.proxyBackup || '';
      const isStale = v => v && v !== EGRESS_DIRECT && !proxyListCache.includes(v);
      const stale = isStale(pMain) || isStale(pBackup);
      // 主 = 全局/直连 时辅跟随锁定（渲染期同样生效）；辅代理开关关闭时同理。
      const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
      const backupVal = backupLocked ? pMain : pBackup;
      const bindCell = '<td style="white-space:nowrap">' +
        egComboHTML('acc', a.accountId, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
        egComboHTML('acc', a.accountId, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
        (stale ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — the account is skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
        '</td>';
      // PoolEnabled 与账号状态是两个维度：未设置时按启用渲染，显式 false
      // 才退出主选号；勾选改动先暂存，由"保存"按钮统一提交。
      const poolEnabled = a.poolEnabled !== false;
      accSnapshot[a.accountId] = { main: pMain, backup: backupVal, enabled: poolEnabled };
      const poolCell = '<td style="text-align:center">' +
        '<input type="checkbox" data-pool-enabled="' + esc(a.accountId) + '"' +
        (poolEnabled ? ' checked' : '') +
        ' title="' + esc(T('Participates in rotation')) + '" aria-label="' + esc(T('Participates in rotation')) + '">' +
        '</td>';
      const st = esc(a.status);
      return '<tr draggable="true" data-drag-id="' + esc(a.accountId) + '">' +
        '<td>' + esc(a.email) + '</td>' +
        '<td><span class="status ' + st + '"><span class="status-dot ' + st + '"></span>' + (sn[a.status] || st) + '</span>' + statusExtra + '</td>' +
          '<td title="Today ' + fmtNum(a.tokensToday) + ' / total ' + fmtNum(a.tokensTotal) + ' tokens (exact when upstream returns usage, otherwise estimated)">' + fmtTokens(a.tokensToday) + ' / ' + fmtTokens(a.tokensTotal) + '</td>' +
        '<td class="mono" style="font-size:11px">' + lu + '</td>' +
        '<td class="mono" style="font-size:11px">' + cr + '</td>' +
        bindCell +
        '<td style="white-space:nowrap">' +
          // Action buttons dispatch via data-* attributes + delegated listeners:
          // inline onclick string concatenation gets HTML-decoded before JS parsing,
          // so even escaped quotes could be bypassed
          '<button class="btn btn-sm" data-act="test" data-acc="' + esc(a.accountId) + '" title="Test whether the account works (success clears cooldown/expired state)">Test</button> ' +
          '<button class="btn btn-sm" data-act="reset" data-acc="' + esc(a.accountId) + '" title="Probe the rate limit and lift it: probes upstream; if still limited, stays in cooldown and shows recovery time">Reset</button> ' +
          '<button class="btn btn-sm btn-danger" data-act="delete" data-acc="' + esc(a.accountId) + '" title="Delete">Delete</button>' +
        '</td>' +
        poolCell +
        '</tr>';
    }).join('');
    tbody.onchange = e => {
      const input = e.target.closest('input[data-pool-enabled]');
      if (!input) return;
      // 暂存模型：勾选改动不直接落盘，行高亮，由"保存"按钮统一提交
      markRowDirty(input.closest('tr'), 'acc');
    };
    tbody.onclick = e => {
      const b = e.target.closest('button[data-act]');
      if (!b) return;
      const id = b.dataset.acc;
      if (b.dataset.act === 'test') testAccount(id, b);
      else if (b.dataset.act === 'reset') resetAccount(id, b);
      else if (b.dataset.act === 'delete') deleteAccount(id);
    };
  } catch (e) { toast(T('Failed to load accounts: ') + e.message, 'error'); }
}

// ========== Proxy binding (account isolation) ==========
// 代理池列表缓存：账号表与 zen key 表的绑定下拉共用。value 是完整代理 URL
//（保存需要，#别名随行持久化），展示名优先用别名，无别名时打码凭据段。
let proxyListCache = [];
// backupProxyOn 辅代理开关（代理池页，持久化在 zen 配置里，默认关闭）。
// 关闭时辅槽整体失效：下拉只读并强制跟随主槽，保存后该身份只剩主出口。
// 默认 false 与后端一致 —— 首次渲染就是关闭态，避免开关未拉到就先解锁。
let backupProxyOn = false;
async function loadProxyListCache() {
  try {
    const d = await api('GET', '/opencode/config');
    proxyListCache = (d.data.proxies || []).slice();
    backupProxyOn = d.data.backupProxyEnabled === true;
    syncProxyHealthCache((d.data.runtime || {}).proxyHealth);
  } catch (e) { /* keep previous cache */ }
}
const maskProxyLabel = p => String(p || '').replace(/\/\/[^@/]*@/, '//***@');
// 每代理"最近在线率"缓存：与代理池页健康表同源（/opencode/config 的
// runtime.proxyHealth）。快照里的 proxy 是打码 URL，与 proxyListCache 的
// 完整 URL 对不上，两侧都剥掉凭据段后再以剩余部分作 key 对齐。
let proxyHealthByProxy = {};
const proxyHealthKey = p => String(p || '').replace(/\/\/[^@/]*@/, '//');
function syncProxyHealthCache(rows) {
  proxyHealthByProxy = {};
  (rows || []).forEach(r => { if (r && r.proxy) proxyHealthByProxy[proxyHealthKey(r.proxy)] = r; });
}
// 下拉行右侧展示的最近在线率：无样本返回 null（不标注）；低在线率标红，
// 阈值与代理池页健康表一致（PROXY_HEALTH_* 见 renderProxyHealth 附近）。
function proxyRecentRate(p) {
  const r = proxyHealthByProxy[proxyHealthKey(p)];
  if (!r || r.recentRate === null || r.recentRate === undefined) return null;
  const low = (r.recentTotal || 0) >= PROXY_HEALTH_MIN_SAMPLES && r.recentRate < PROXY_HEALTH_LOW_RATE;
  return { text: Math.round(r.recentRate * 100) + '%', low: low };
}

// ========== 账号表暂存编辑（三平台共用） ==========
// 代理绑定与"启用"勾选的改动只落在 DOM（行高亮 + 保存按钮描边），点击各
// 平台的"保存"按钮才逐行 diff 快照后提交后端，再刷新表格。清空代理/一键
// 启用/一键禁用同样只是暂存，保存后才生效。
const EDIT_SAVE_BTN = { acc: 'accSaveBtn', key: 'ocKeysSaveBtn', wb: 'wbSaveBtn' };
function markRowDirty(row, kind) {
  if (row) row.classList.add('tr-dirty');
  const btn = _(EDIT_SAVE_BTN[kind] || 'accSaveBtn');
  if (btn) btn.classList.add('dirty-hint');
}
function clearDirtyMarks(tbodyId, btnId) {
  document.querySelectorAll('#' + tbodyId + ' tr.tr-dirty').forEach(tr => tr.classList.remove('tr-dirty'));
  const btn = _(btnId);
  if (btn) btn.classList.remove('dirty-hint');
}
// 清空单行代理绑定（暂存）：主=全局时辅自动跟随锁定
function stageRowBindingClear(row, kind) {
  const main = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]');
  if (!main) return false;
  if (!main.dataset.value) return false;
  main.dataset.value = '';
  main.value = egLabel('');
  egApplyMainLinkage(row, '');
  return true;
}

let accSnapshot = {};
function stageAllAccountsPool(enabled) {
  document.querySelectorAll('#accountTableBody input[data-pool-enabled]').forEach(cb => {
    const snap = accSnapshot[cb.dataset.poolEnabled];
    if (snap && snap.enabled === enabled) return; // 与已保存状态一致，不产生待保存项
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'acc');
  });
}
function stageClearAccountProxies() {
  document.querySelectorAll('#accountTableBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="acc"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'acc');
    const cb = row.querySelector('input[data-pool-enabled]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'acc');
  });
}
async function saveAccountEdits() {
  const rows = [...document.querySelectorAll('#accountTableBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="acc"][data-slot="main"]');
    const id = mainInput.dataset.egid;
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="acc"][data-slot="backup"]').dataset.value || '';
    const cb = row.querySelector('input[data-pool-enabled]');
    const snap = accSnapshot[id] || { main: '', backup: '', enabled: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/accounts/proxy', { accountId: id, main: main, backup: backup });
      }
      if (cb && cb.checked !== snap.enabled) {
        await api('POST', '/accounts/pool/enabled', { accountId: id, poolEnabled: cb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (cb && cb.checked !== snap.enabled)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('accountTableBody', 'accSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadAccounts(); loadStats();
}

// 复位：丢弃全部暂存修改，按上次保存的状态重绘表格
function resetAccountEdits() {
  clearDirtyMarks('accountTableBody', 'accSaveBtn');
  loadAccounts();
}

// ========== 添加帐号弹窗（复用导入页的类型自动识别与后端接口） ==========
function openAccAddDialog() {
  _('accAddToken').value = '';
  _('accAddEmail').value = '';
  _('accAddDialog').showModal();
}
function closeAccAddDialog() { _('accAddDialog').close(); }
async function accAddFromDialog() {
  const token = _('accAddToken').value.trim();
  if (!token) { toast('Enter a token or API key', 'error'); return; }
  const email = _('accAddEmail').value.trim();
  const btn = _('accAddSubmit');
  btn.disabled = true;
  try {
    const d = await api('POST', '/accounts/add', { ...credPayload(token), email: email || undefined });
    toast(T('Account added: ') + (d.data.email || ''), 'success');
    closeAccAddDialog();
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Add failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

// 批量测试：等价于对每个账号点一次 Test（真实探测，成功即复位冷却）。
// 逐个串行执行避免打爆上游；按钮上显示进度，结束后汇总一条 toast。
async function batchTestAccounts() {
  const btn = _('accTestBtn');
  const ids = Object.keys(accSnapshot);
  if (!ids.length) { toast(T('No accounts to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < ids.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + ids.length;
      try {
        const d = await api('POST', '/accounts/test', { accountId: ids[i] });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + ids.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadAccounts(); loadStats();
}

function stageClearZenKeyProxies() {
  if (!confirm(T('Clear proxy bindings on ALL keys and disable routing? Click Save to apply.'))) return;
  document.querySelectorAll('#ocKeysBody tr').forEach(row => {
    const main = row.querySelector('.eg-input[data-eg="key"][data-slot="main"]');
    if (!main) return;
    let dirty = stageRowBindingClear(row, 'key');
    const cb = row.querySelector('input[data-zr]');
    if (cb && cb.checked) { cb.checked = false; dirty = true; }
    if (dirty) markRowDirty(row, 'key');
  });
}

// ========== 代理套用（把本页绑定按行序复制到其余两个账号池）==========
// 页名 → 绑定 kind 与表格选择器：owner 页的绑定从 DOM 读（含暂存未保存的），
// 其余两页按行序覆盖第 0..n-1 行；异页缺号保留原绑定不动，多出来的丢弃。
// 只写代理绑定，不碰启用/禁用/路由，不走 Save 暂存流程，写入即落盘。
const BINDING_PAGES = [
  { kind: 'acc', name: 'Cline',  tbody: '#accountTableBody' },
  { kind: 'key', name: 'OpenCode', tbody: '#ocKeysBody' },
  { kind: 'wb',  name: 'WorkBuddy', tbody: '#wbAccountsBody' }
];
// 读一行的主/辅绑定：辅槽在开关关闭/主为全局或直连时已被 egApplyMainLinkage
// 锁定跟随主槽，dataset.value 即为最终生效值，照抄即可。
function readRowBindings(row, kind) {
  const main = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]');
  if (!main) return null;
  const backup = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="backup"]');
  return { id: main.dataset.egid, main: main.dataset.value || '', backup: backup ? (backup.dataset.value || '') : '' };
}
// 写一行的绑定（仅代理）：同步 DOM 的 data-value 与显示标签，辅槽联动规则照旧。
function setRowBindings(row, kind, main, backup) {
  const mainInput = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="main"]');
  if (!mainInput) return null;
  mainInput.dataset.value = main;
  mainInput.value = egLabel(main);
  const backupInput = row.querySelector('.eg-input[data-eg="' + kind + '"][data-slot="backup"]');
  const bVal = (!backupProxyOn || main === '' || main === EGRESS_DIRECT) ? main : backup;
  if (backupInput) {
    backupInput.dataset.value = bVal;
    backupInput.value = egLabel(bVal);
  }
  egApplyMainLinkage(row, main);
  return mainInput.dataset.egid;
}
// 目标页落盘：与各页 Save 的提交口径一致（绑定与启用分开提交，这里只发绑定）。
async function writeBinding(kind, id, main, backup) {
  if (kind === 'acc') {
    await api('POST', '/accounts/proxy', { accountId: id, main: main, backup: backup });
  } else if (kind === 'key') {
    await api('POST', '/opencode/keys/proxy', { index: parseInt(id, 10), main: main, backup: backup });
  } else {
    await workbuddyProxyAdminCall('POST', 'proxy/set', { uid: id, main: main, backup: backup });
  }
}
async function applyProxiesToOthers(ownerKind) {
  const owner = BINDING_PAGES.find(p => p.kind === ownerKind);
  if (!owner) return;
  const ownerRows = [...document.querySelectorAll(owner.tbody + ' tr')]
    .filter(r => r.querySelector('.eg-input[data-slot="main"]'));
  const bindings = ownerRows.map(r => readRowBindings(r, ownerKind)).filter(Boolean);
  if (!bindings.length) { toast(T('No accounts to apply from'), 'info'); return; }
  const targets = BINDING_PAGES.filter(p => p.kind !== ownerKind);
  if (!confirm(T('Apply ') + owner.name + T(' proxy bindings by row order to ') +
    targets.map(p => p.name).join(T(' and ')) + T(' accounts? Takes effect immediately; ') +
    T('only proxy bindings change, enable/disable states are untouched.'))) return;
  let ok = 0, fail = 0;
  const refreshPending = { key: false, wb: false };
  for (const t of targets) {
    const rows = [...document.querySelectorAll(t.tbody + ' tr')]
      .filter(r => r.querySelector('.eg-input[data-slot="main"]'));
    for (let i = 0; i < rows.length && i < bindings.length; i++) {
      const id = setRowBindings(rows[i], t.kind, bindings[i].main, bindings[i].backup);
      if (id == null) continue;
      try {
        await writeBinding(t.kind, id, bindings[i].main, bindings[i].backup);
        ok++;
      } catch (e) { fail++; }
    }
    if (t.kind === 'key') refreshPending.key = true;
    if (t.kind === 'wb') refreshPending.wb = true;
  }
  // 目标页表格重拉确认落盘结果；owner 页保持原样（其自身表格未被改动）。
  // 切走页面导致刷新失败时保持简单，不打断套用流程。
  if (refreshPending.key) { try { await loadOcConfig(); } catch (e) { console.warn('applyProxiesToOthers: reload OpenCode keys failed', e); } }
  if (refreshPending.wb) { try { await loadWorkbuddyAccounts(); } catch (e) { console.warn('applyProxiesToOthers: reload WorkBuddy accounts failed', e); } }
  if (fail) toast(T('Applied ') + ok + T(' binding(s), ') + fail + T(' failed'), 'warning');
  else toast(T('Applied ') + ok + T(' binding(s) to other pools'), 'success');
}

// ========== 出口绑定联想下拉（combobox） ==========
// 选项固定为 全局 / 直连，其后是代理池按输入联想。联想阈值：有效字符 ≥3
//（中日韩字符按 1.5 计，即 2 个汉字触发）。value 语义：'' = 全局（默认，
// 隔离模式下沿用全局规则）；'direct' = 强制直连；代理 URL = 绑定出口。
const EGRESS_DIRECT = 'direct';
// 代理展示名：优先别名（URL 的 #fragment 随行持久化），无别名时打码凭据段。
// 绑定下拉、冷却状态等只显示别名，不暴露完整地址。
function egLabel(v) {
  if (!v) return T('Global');
  if (v === EGRESS_DIRECT) return T('Direct');
  try {
    const h = new URL(v).hash;
    if (h && h.length > 1) return decodeURIComponent(h.slice(1));
  } catch (e) { /* 非 URL 形态，回退打码 */ }
  return maskProxyLabel(v);
}
function egComboHTML(kind, id, slot, value, title, disabled) {
  // 只读态的原因决定提示文案：辅代理开关关闭 ≠ 主槽是全局/直连
  const t = disabled
    ? T(slot === 'backup' && !backupProxyOn ? 'Backup proxies are disabled — follows the main slot' : 'Follows the main slot')
    : title;
  return '<input type="text" class="eg-input" data-eg="' + kind + '" data-egid="' + esc(id) + '" data-slot="' + slot + '" data-value="' + esc(value || '') + '" value="' + esc(egLabel(value)) + '" title="' + esc(t) + '"' + (disabled ? ' disabled' : '') + ' autocomplete="off" spellcheck="false">';
}
// 主槽位联动：主 = 全局/直连 时辅自动跟随相同值并只读；主 = 代理时辅解锁。
// 辅代理开关关闭时辅槽无条件只读并跟随主槽 —— 关闭态下辅槽不参与出口选择，
// 留着可编辑只会让用户以为绑定生效了。
// 被锁定期间辅的旧值会被跟随值覆盖（解锁后辅回到"全局"，可重新选择）。
function egApplyMainLinkage(row, mainValue) {
  const backup = row ? row.querySelector('.eg-input[data-slot="backup"]') : null;
  if (!backup) return;
  if (!backupProxyOn || mainValue === '' || mainValue === EGRESS_DIRECT) {
    backup.dataset.value = mainValue;
    backup.value = egLabel(mainValue);
    backup.disabled = true;
    backup.title = T(!backupProxyOn ? 'Backup proxies are disabled — follows the main slot' : 'Follows the main slot');
  } else {
    backup.disabled = false;
    backup.title = T('Backup exit — used when main is cooling/removed');
  }
}
function egCloseMenu() { const m = document.getElementById('egMenu'); if (m) m.remove(); }
function egOpenMenu(input) {
  egCloseMenu();
  const menu = document.createElement('div');
  menu.id = 'egMenu';
  menu.className = 'eg-menu';
  input.dataset.q = '';
  input.select();
  menu.addEventListener('mousedown', e => {
    const item = e.target.closest('.eg-item[data-value]');
    if (!item) return;
    e.preventDefault();
    egCommit(input, item.dataset.value);
  });
  document.body.appendChild(menu);
  egPositionMenu(input, menu);
  egRenderItems(input, menu, '');
}
function egPositionMenu(input, menu) {
  const r = input.getBoundingClientRect();
  menu.style.left = Math.min(r.left, window.innerWidth - 240) + window.scrollX + 'px';
  menu.style.top = r.bottom + window.scrollY + 4 + 'px';
  menu.style.minWidth = Math.max(r.width, 220) + 'px';
}
function egRenderItems(input, menu, q) {
  const cur = input.dataset.value || '';
  const items = [
    { v: '', label: T('Global'), active: cur === '' },
    { v: EGRESS_DIRECT, label: T('Direct'), active: cur === EGRESS_DIRECT }
  ];
  // 联想阈值：输入含中文时中文字符 ≥2 触发，否则长度 ≥3；未达标不展示代理
  const cjk = [...q].filter(c => c.codePointAt(0) > 0x7f).length;
  const ready = q === '' || (cjk > 0 ? cjk >= 2 : q.length >= 3);
  const ql = q.toLowerCase();
  const matches = p => {
    const label = egLabel(p).toLowerCase();
    return label.includes(ql) || p.toLowerCase().includes(ql);
  };
  const proxyItem = p => ({ v: p, label: egLabel(p), active: cur === p, rate: proxyRecentRate(p) });
  const proxies = q === ''
    ? proxyListCache.map(proxyItem)
    : (ready
        ? proxyListCache.filter(matches).map(proxyItem)
        : []);
  let html = items.concat(proxies).map(it =>
    '<div class="eg-item' + (it.active ? ' active' : '') + '" data-value="' + esc(it.v) + '">' + esc(it.label) +
    (it.rate ? '<span class="eg-rate"' + (it.rate.low ? ' style="color:var(--danger);font-weight:600"' : '') + '>' + it.rate.text + '</span>' : '') +
    '</div>'
  ).join('');
  if (q !== '' && !proxies.length) html += '<div class="eg-item eg-hint">' + T('No matching proxies') + '</div>';
  menu.innerHTML = html;
}
function egCommit(input, value) {
  input.dataset.value = value;
  input.value = egLabel(value);
  egCloseMenu();
  input.blur();
  if (input.dataset.slot === 'main') egApplyMainLinkage(input.closest('tr'), value);
  // 暂存模型：绑定改动只落 DOM 并高亮行，由各平台"保存"按钮统一提交
  markRowDirty(input.closest('tr'), input.dataset.eg);
}
document.addEventListener('focusin', e => {
  if (e.target.classList && e.target.classList.contains('eg-input')) egOpenMenu(e.target);
});
document.addEventListener('input', e => {
  if (!e.target.classList || !e.target.classList.contains('eg-input')) return;
  const menu = document.getElementById('egMenu');
  if (menu) egRenderItems(e.target, menu, e.target.value);
  else egOpenMenu(e.target);
});
document.addEventListener('keydown', e => { if (e.key === 'Escape') egCloseMenu(); });
document.addEventListener('blur', e => {
  if (e.target.classList && e.target.classList.contains('eg-input')) {
    // 未选择直接失焦：显示还原为当前绑定值的标签
    e.target.value = egLabel(e.target.dataset.value);
    setTimeout(egCloseMenu, 150);
  }
}, true);

async function testAccount(id, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/accounts/test', { accountId: id });
    const r = d.data || {};
    const statusMap = { active: 'Available', cooldown: 'Cooldown', expired: 'Expired', error: 'Error' };
    const label = statusMap[r.status] || r.status;
    const prevMap = { active: 'Active', cooldown: 'Cooldown', expired: 'Expired', '': '' };
    // toast 用 textContent 渲染，参数一律不要再 esc()：转义会以字面量显示
    //（429 的错误体就变成可见的 &quot;error&quot;:…）
    let msg = T('Account ') + (r.email || '') + T(' — ') + label;
    if (r.prevStatus && r.prevStatus !== r.status) msg += T(' (was: ') + (prevMap[r.prevStatus] || r.prevStatus) + T(')');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil);
    if (r.remaining) msg += T(' (remaining ') + r.remaining + T(')');
    // 成功时不显示"原因 ok / HTTP 200"这类噪音：只有非成功结果才需要原因
    if (r.status !== 'active') {
      if (r.reason) msg += T('\nReason: ') + r.reason;
      if (r.httpStatus) msg += T('\nHTTP: ') + r.httpStatus;
    }
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
}

async function deleteAccount(id) {
  if (!confirm(T('Delete this account?'))) return;
  try {
    await api('POST', '/accounts/delete', { accountId: id });
    toast('Account deleted', 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function resetAccount(id, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Checking'; }
  try {
    const d = await api('POST', '/accounts/reset', { accountId: id });
    const r = d.data || {};
    const type = d.success ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    let msg = d.message || T('Check complete');
    // 恢复时刻与剩余时长都由面板渲染：服务器发的是 RFC3339 时刻（浏览器本地
    // 时区显示），remaining 只在这里加一次，避免与服务器消息重复。
    // toast 走 textContent，不要再 esc()（会显示成字面量 &quot;）
    if (r.cooldownUntil && r.status === 'cooldown') msg += T(' (estimated recovery ') + fmtWhen(r.cooldownUntil) + T(')');
    if (r.remaining && r.status !== 'active') msg += T(' (remaining ') + r.remaining + T(')');
    toast(msg, type, 6000);
    loadAccounts(); loadStats();
  } catch (e) {
    toast(T('Check failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
}

// ========== Config export / import ==========
let importPayload = null;

async function exportConfig() {
  try {
    const res = await fetch(API + '/config/export');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'afree-proxy-config-' + new Date().toISOString().slice(0, 10) + '.json';
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
    toast(T('Config exported'), 'success');
  } catch (e) { toast(T('Export failed: ') + e.message, 'error'); }
}

function onImportFileChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    try {
      const parsed = JSON.parse(reader.result);
      if (parsed.version !== 1) throw new Error(T('Unsupported backup version'));
      importPayload = JSON.stringify(parsed);
      importConfigFromFile();
    } catch (e) {
      importPayload = null;
      toast(T('Invalid backup file: ') + e.message, 'error');
    }
  };
  reader.readAsText(file);
  event.target.value = '';
}

async function importConfigFromFile() {
  if (!importPayload) { toast(T('Choose a backup JSON file first'), 'error'); return; }
  if (!confirm(T('Import replaces sections present in the file (accounts / keys / proxies / combos ...). Continue?'))) return;
  try {
    const d = await api('POST', '/config/import', JSON.parse(importPayload));
    const imp = (d.data && d.data.imported) || {};
    const parts = Object.keys(imp).map(k => k + '=' + imp[k]);
    toast(T('Config imported') + ' — ' + parts.join(', '), 'success');
    setTimeout(() => location.reload(), 1200);
  } catch (e) { toast(T('Import failed: ') + e.message, 'error'); }
}

async function deleteAllAccounts() {
  if (!confirm(T('Delete ALL accounts? This cannot be undone!'))) return;
  try {
    await api('POST', '/accounts/delete-all', {});
    toast('All accounts deleted', 'success');
    loadAccounts(); loadStats();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

// ========== Token import ==========
// Credential type auto-detection: "sk_..." = static Cline API key, anything
// else = OAuth refreshToken (the backend validates refresh tokens upstream;
// API keys are pooled as-is).
const credPayload = t => t.startsWith('sk_') ? { apiToken: t } : { refreshToken: t };

// ========== API keys ==========
async function loadKeys() {
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys;
    const el = _('keysList');
    if (!keys || keys.length === 0) {
      el.innerHTML = '<div class="empty-state"><span class="icon"><svg viewBox="0 0 24 24" width="36" height="36" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2l-2 2m-7.6 7.6a5.5 5.5 0 1 1-7.78 7.78 5.5 5.5 0 0 1 7.78-7.78zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/></svg></span>No API keys yet</div>';
      return;
    }
    el.innerHTML = keys.map(k =>
      '<div class="flex" style="margin-bottom:8px">' +
        '<span class="key-display" style="flex:1" data-copy="' + esc(k) + '" title="Click to copy">' + esc(k) + '</span>' +
        '<button class="btn btn-sm btn-danger" data-delkey="' + esc(k) + '">Delete</button>' +
      '</div>'
    ).join('');
    el.onclick = e => {
      const c = e.target.closest('[data-copy]');
      if (c) { copyText(c.dataset.copy); return; }
      const d = e.target.closest('button[data-delkey]');
      if (d) deleteKey(d.dataset.delkey);
    };
  } catch (e) { _('keysList').innerHTML = '<div class="empty">Failed to load</div>'; }
}

async function generateKey() {
  try {
    const d = await api('POST', '/keys/generate');
    const key = d.data.key;
    _('keyGenResult').innerHTML =
      '<div style="background:rgba(52,211,153,.08);border:1px solid rgba(52,211,153,.4);border-radius:10px;padding:12px">' +
        '<div style="color:var(--accent2);font-weight:600;margin-bottom:8px">New key generated (click to copy)</div>' +
        '<div class="key-display" data-copy="' + esc(key) + '">' + esc(key) + '</div>' +
      '</div>';
    const kr = _('keyGenResult').querySelector('[data-copy]');
    if (kr) kr.onclick = () => copyText(kr.dataset.copy);
    loadKeys();
    toast('Key generated', 'success');
    setTimeout(() => _('keyGenResult').innerHTML = '', 8000);
  } catch (e) { toast(T('Generation failed: ') + e.message, 'error'); }
}

async function deleteKey(key) {
  if (!confirm(T('Delete this key?'))) return;
  try {
    await api('POST', '/keys/delete', { key });
    toast('Key deleted', 'success');
    loadKeys();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function deleteAllKeys() {
  if (!confirm(T('Delete ALL API keys?'))) return;
  try {
    const d = await api('GET', '/keys');
    const keys = d.data.keys || [];
    for (const k of keys) await api('POST', '/keys/delete', { key: k });
    toast('All keys deleted', 'success');
    loadKeys();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function deleteAllData() {
  if (!confirm(T('Delete ALL persisted data? This will clear Cline accounts and client keys, OpenCode keys, WorkBuddy accounts, the proxy pool, default model, request headers, scheduling strategy, custom aliases, sticky sessions, and request logs. This cannot be undone!'))) return;
  try {
    await api('POST', '/data/delete-all', {});
    toast(T('All persisted data deleted'), 'success');
    setTimeout(() => location.reload(), 1000);
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

function copyText(t, okMsg) {
  const done = () => toast(okMsg || 'Copied to clipboard', 'success');
  // clipboard API 只在 secure context（https/localhost）可用；navigator.clipboard 为
  // undefined 时直接调用会同步抛错，.catch 接不住，必须先判空再走 execCommand 降级。
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(t).then(done, () => { legacyCopy(t); done(); });
    return;
  }
  legacyCopy(t); done();
}
function legacyCopy(t) {
  const ta = document.createElement('textarea');
  ta.value = t; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove();
}

// ========== Request logs ==========
const ROUTE_LABEL = { zen: 'opencode', cline: 'cline pool', admin: 'admin', meta: 'meta', other: 'other' };
const PROXY_LABEL = { main: 'Main', backup: 'Backup', direct: 'Direct' };
const STATUS_CLASS = s => s >= 500 ? 'color:var(--danger)' : (s >= 400 ? 'color:var(--amber)' : 'color:var(--accent2)');
let logsAuto = true;

function toggleLogsAuto() {
  logsAuto = !logsAuto;
  _('logsAutoBtn').textContent = logsAuto ? 'Pause' : 'Resume';
  const pill = _('logsAutoPill');
  pill.textContent = logsAuto ? 'Auto-refreshing' : 'Paused';
  pill.classList.toggle('paused', !logsAuto);
  if (logsAuto) loadLogs();
}

async function loadLogs() {
  const tbody = _('logsTableBody');
  try {
    const d = await api('GET', '/logs');
    const logs = d.data.logs || [];
    if (!logs.length) { tbody.innerHTML = '<tr><td colspan="10" class="empty">No requests logged</td></tr>'; return; }
    tbody.innerHTML = logs.map(l => {
      const t = l.time ? new Date(l.time).toLocaleString('en-US') : '-';
      const route = ROUTE_LABEL[l.route] || l.route || '-';
      const st = l.status || 0;
      const proxy = PROXY_LABEL[l.proxyType] || '-';
      return '<tr>' +
        '<td class="mono" style="font-size:11px">' + t + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.client || '-') + '</td>' +
        '<td>' + esc(l.method || '-') + '</td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.path || '-') + '</td>' +
        '<td class="mono" style="font-size:12px">' + esc(l.model || '-') + '</td>' +
        '<td><span class="model-tag">' + esc(route) + '</span></td>' +
        '<td class="mono" style="font-size:11px">' + esc(l.upstream || '-') + '</td>' +
        '<td>' + esc(proxy) + '</td>' +
        '<td style="font-weight:600;color:' + STATUS_CLASS(st) + '">' + st + '</td>' +
        '<td class="mono" style="font-size:11px">' + (l.duration_ms != null ? l.duration_ms + ' ms' : '-') + '</td>' +
      '</tr>';
    }).join('');
  } catch (e) { tbody.innerHTML = '<tr><td colspan="10" class="empty">Failed to load</td></tr>'; }
}

// ========== Config updates ==========
async function updateConfig() {
  const strategy = _('settingStrategy').value;
  try {
    await api('POST', '/config/update', { strategy });
    toast(T('Strategy updated to: ') + strategy, 'success');
  } catch (e) { toast(T('Update failed: ') + e.message, 'error'); }
}

// saveAdminLogToggle 勾选即存：/admin 页面与接口访问是否写入请求日志。
// 关闭（默认）只影响面板自身产生的行，经过网关的模型会话请求日志不受影响。
async function saveAdminLogToggle() {
  const cb = _('settingAdminLog');
  try {
    await api('POST', '/config/update', { adminLogEnabled: cb.checked });
    toast(cb.checked ? 'Admin access logging enabled' : 'Admin access logging disabled', 'success');
  } catch (e) {
    cb.checked = !cb.checked;  // 保存失败回滚勾选，避免界面与落盘状态不一致
    toast(T('Save failed: ') + e.message, 'error');
  }
}

function addHeaderRow() {
  const tbody = _('headersTableBody');
  const tr = document.createElement('tr');
  tr.innerHTML =
    '<td><input type="text" class="header-key" placeholder="Header-Name" style="font-size:12px;font-family:monospace"></td>' +
    '<td><input type="text" class="header-val" placeholder="value" style="font-size:12px;font-family:monospace"></td>' +
    '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">Delete</button></td>';
  tbody.appendChild(tr);
}

async function resetHeaders() {
  try {
    await api('POST', '/headers/reset', {});
    toast(T('Headers restored to defaults'), 'success');
    loadConfig();
  } catch (e) { toast(T('Reset failed: ') + e.message, 'error'); }
}

async function saveHeaders() {
  const tbody = _('headersTableBody');
  const rows = tbody.querySelectorAll('tr');
  const headers = {};
  let hasEmpty = false;
  rows.forEach(tr => {
    const keyInput = tr.querySelector('.header-key');
    const valInput = tr.querySelector('.header-val');
    if (keyInput && valInput) {
      const k = keyInput.value.trim();
      const v = valInput.value.trim();
      if (k) { headers[k] = v; }
      else if (v) { hasEmpty = true; }
    }
  });
  if (hasEmpty) { toast('Rows with a value but no key were ignored', 'info'); }
  try {
    const d = await api('POST', '/config/update', { headers });
    toast('Headers saved', 'success');
    _('headerSaveResult').innerHTML =
      '<div style="color:var(--accent2);font-size:12px">' + T('Saved ') + Object.keys(d.data.headers).length + T(' headers') + '</div>';
    setTimeout(() => _('headerSaveResult').innerHTML = '', 5000);
    loadConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

const WB_RATE_OPTIONS = [0, 0.1, 0.2, 0.3, 0.5, 1.0];
// 倍率筛选阈值：服务端配置（wbconfig pool.model_rate_filter）持有，网关
// /v1/models 与面板同口径过滤。初值 0.2 只是首屏未加载完的占位——loadModels /
// loadWorkbuddyConfig 拉到真值后立刻覆盖，落盘由 setWorkbuddyRateFilter 负责。
let workbuddyRateFilter = 0.2;
function wbNumber(value) {
  const n = Number(String(value ?? '').replace(/[^\d.-]/g, ''));
  return Number.isFinite(n) ? n : null;
}
function wbRate(m) {
  const promoted = m && m.promo_factor !== undefined && m.promo_factor !== null &&
    m.promo_credits !== undefined && m.promo_credits !== null && m.promo_credits !== '';
  return wbNumber(promoted ? m.promo_credits : m && m.credits);
}
// 倍率缺失（null）的模型一律隐藏：缺失 ≠ 免费（试用模型显式清空倍率、目录
// 未刷新时整表缺失、上游没给 credits，都可能是收费模型），无法证明 ≤ 阈值就
// 不展示，与服务端 matchesModelRateFilter 同口径。
function wbModelMatchesFilter(m) {
  const rate = wbRate(m);
  return rate !== null && rate <= workbuddyRateFilter + 1e-9;
}
async function fetchWorkbuddyModels() {
  try {
    const data = await wbCall('models');
    return data.models || [];
  } catch (e) {
    return [];
  }
}
function wbContextLabel(value) {
  const n = Number(value || 0) / 1000;
  if (!(n > 0)) return '—';
  const tiers = [[32, '32K'], [64, '64K'], [128, '128K'], [256, '256K'], [512, '512K'], [1024, '1M'], [2048, '2M']];
  let best = tiers[0];
  tiers.forEach(t => {
    if (Math.abs(n - t[0]) <= Math.abs(n - best[0])) best = t;
  });
  return best[1];
}
function wbContextTagHTML(value, title) {
  const large = Number(value || 0) >= 1000000;
  return '<span class="model-tag' + (large ? ' context-1m' : '') + '" title="' + esc(title) + '">' + esc(wbContextLabel(value)) + '</span>';
}
function wbRateHTML(m) {
  const rate = wbRate(m);
  const base = wbNumber(m.credits);
  const value = rate === null ? '—' : 'x' + rate;
  const old = base !== null && rate !== null && base !== rate ? ' <s style="color:var(--text3)">x' + esc(base) + '</s>' : '';
  const promo = m.promo_label ? ' <span class="model-tag">' + esc(m.promo_label) + '</span>' : '';
  return '<span title="' + esc(m.promo_note || '') + '">' + value + old + promo + '</span>';
}
// setWorkbuddyRateFilter 改倍率筛选：先写服务端配置（wbconfig
// pool.model_rate_filter，经 livecfg 热生效，/v1/models 立刻按新阈值过滤），
// 落盘成功再刷本地视图。保存失败保持旧值并报错——面板显示必须与服务端一致，
// 否则用户以为改了、网关还在按旧阈值出名单。
async function setWorkbuddyRateFilter(value) {
  const next = Number(value);
  const applied = WB_RATE_OPTIONS.includes(next) ? next : 0.2;
  try {
    await wbCall('config', {}, { pool: { model_rate_filter: applied } });
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
    return;
  }
  workbuddyRateFilter = applied;
  loadModels();
  loadModelOptions();
  comboModels.workbuddy = [];
  if (_('comboPlatform') && _('comboPlatform').value === 'workbuddy') {
    fillComboModels();
  }
}

async function loadModels() {
  try {
    // 倍率阈值先于渲染拉取：它决定 wb 名单与下拉选中项（服务端配置，
    // wbconfig pool.model_rate_filter）。
    await syncWorkbuddyRateFilter();
    const d = await api('GET', '/models');
    const models = d.data.models || [];
    let info = '';
    if (d.data.lastSync) info += T('· official feed: ') + new Date(d.data.lastSync).toLocaleTimeString('en-US');
    _('modelsProbeInfo').textContent = info;
    // OpenCode 免费模型（网关设置页分两块展示：Cline 在上、OpenCode 在下）
    const [zen, wbAll] = await Promise.all([
      api('GET', '/opencode/models').then(r => r.data.models || []).catch(() => []),
      fetchWorkbuddyModels()
    ]);
    const row = (inner) => '<div style="display:flex;align-items:center;gap:10px;padding:8px 12px;margin:5px 0;background:rgba(148,163,184,.06);border:1px solid var(--border);border-radius:10px;transition:.15s">' + inner + '</div>';
    const clineBlock = models.length
      ? models.map(m => {
          return row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>');
        }).join('')
      : '<div class="empty">' + T('No models') + '</div>';
    const zenBlock = zen.length
      ? zen.map(m => row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1">' + esc(m.id) + '</span>' +
            '<span class="model-tag free">' + T('no charge') + '</span>' +
            wbContextTagHTML(m.context, T('Context')))).join('')
      : '<div class="empty">' + T('No models') + '</div>';
    const wbModels = wbAll.filter(wbModelMatchesFilter).sort((a, b) => wbRate(a) - wbRate(b));
    const rateOptions = WB_RATE_OPTIONS.map(v =>
      '<option value="' + v + '"' + (v === workbuddyRateFilter ? ' selected' : '') + '>' + v + '</option>').join('');
    const wbBlock = wbModels.length
      ? wbModels.map(m => {
          const rate = wbRate(m);
          const free = rate === 0 ? '<span class="model-tag free">' + T('free') + '</span>' : '';
          return row(
            '<span style="font-family:\'JetBrains Mono\',monospace;font-size:13px;flex:1" title="' + esc(m.description || '') + '">' + esc(m.id) + '</span>' +
            free +
            '<span class="model-tag">' + wbRateHTML(m) + '</span>' +
            '<span class="model-tag" title="' + T('Default tier') + '">' + esc(m.default_effort || '—') + '</span>' +
            wbContextTagHTML(m.context_length, T('Maximum context length')));
        }).join('')
      : '<div class="empty">' + T('No WorkBuddy models under the current filter') + '</div>';
    _('modelsList').innerHTML =
      '<div style="font-size:12px;font-weight:600;margin:2px 0 8px">' + T('Cline free models') + '</div>' + clineBlock +
      '<div style="font-size:12px;font-weight:600;margin:14px 0 8px">' + T('OpenCode free models') + '</div>' + zenBlock +
      '<div style="display:flex;align-items:center;gap:10px;margin:14px 0 8px">' +
        '<span style="font-size:12px;font-weight:600">'+ T('WorkBuddy models') + '</span><span style="flex:1"></span>' +
        '<label class="inline-flex" style="font-size:12px;color:var(--text2)">' + T('Filter') + ' ' +
          '<select id="wbRateFilter" onchange="setWorkbuddyRateFilter(this.value)" title="' + esc(T('Model rate cap for WorkBuddy models. Applies to the model list and to GET /v1/models responses; saves immediately.')) + '" style="width:auto;padding:4px 26px 4px 9px;font-size:12px">' + rateOptions + '</select></label>' +
      '</div>' + wbBlock;
  } catch (e) { _('modelsList').textContent = T('Failed to load'); }
}

async function refreshModels() {
  try {
    _('modelsProbeInfo').textContent = '· syncing...';
    // 两个上游的模型源一起刷新（Cline feed + OpenCode 目录）
    const [d, z] = await Promise.all([
      api('POST', '/models/refresh').catch(e => ({ message: e.message })),
      api('POST', '/opencode/models/refresh').catch(e => ({ message: e.message }))
    ]);
    const msg = [d.message || (d.data && d.data.message), z.message || (z.data && z.data.message)].filter(Boolean).join(' | ');
    toast(msg || 'Sync started', 'info');
    setTimeout(loadModels, 3000);
  } catch (e) { toast(T('Refresh failed: ') + e.message, 'error'); _('modelsProbeInfo').textContent = ''; }
}

async function loadModelOptions() {
  try {
    // 默认模型下拉：合并 Cline、OpenCode 与当前筛选下的 WorkBuddy 模型。
    const [d, z, wbAll] = await Promise.all([
      api('GET', '/models'),
      api('GET', '/opencode/models').catch(() => ({ data: { models: [] } })),
      fetchWorkbuddyModels()
    ]);
    const platform = _('settingModelPlatform') ? _('settingModelPlatform').value : 'cline';
    const cline = platform === 'cline' ? (d.data.models || []) : [];
    const zen = platform === 'zen' ? (z.data.models || []) : [];
    const wb = platform === 'workbuddy'
      ? wbAll.filter(wbModelMatchesFilter).sort((a, b) => wbRate(a) - wbRate(b))
      : [];
    const sel = _('settingDefModel');
    if (!sel) return;
    // 仅原样展示模型 ID，不附加平台/状态/倍率后缀
    sel.innerHTML = cline.concat(zen, wb).map(m =>
      '<option value="' + esc(m.id) + '">' + esc(m.id) + '</option>').join('');
    if (!sel.options.length) {
      sel.innerHTML = '<option value="">No models on this platform</option>';
      return;
    }
    const c = await api('GET', '/config');
    if (c.data.defaultModel && Array.from(sel.options).some(o => o.value === c.data.defaultModel)) {
      sel.value = c.data.defaultModel;
    } else {
      sel.selectedIndex = 0;
    }
  } catch (e) { /* ignore */ }
}

async function saveDefaultModel() {
  const v = _('settingDefModel').value;
  if (!v) { toast('Select a model', 'error'); return; }
  try {
    const d = await api('POST', '/config/update', { defaultModel: v });
    toast(T('Default model saved: ') + d.data.defaultModel, 'success');
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// ========== Config load ==========
async function loadConfig() {
  try {
    const d = await api('GET', '/config');
    const c = d.data;
    if (c.address) _('settingAddr').value = c.address;
    if (c.strategy) _('settingStrategy').value = c.strategy;
    if (_('settingAdminLog')) _('settingAdminLog').checked = !!c.adminLogEnabled;
    if (c.poolPath && _('settingPoolPath')) _('settingPoolPath').value = c.poolPath;
    loadModelOptions();
    if (c.headers) {
      const tbody = _('headersTableBody');
      tbody.innerHTML = Object.entries(c.headers).map(([k, v]) =>
        '<tr>' +
          '<td><input type="text" class="header-key" value="' + esc(k) + '" style="font-size:12px;font-family:monospace;width:100%"></td>' +
          '<td><input type="text" class="header-val" value="' + esc(v) + '" style="font-size:12px;font-family:monospace;width:100%"></td>' +
          '<td><button class="btn btn-sm btn-danger" onclick="this.closest(\'tr\').remove()">Delete</button></td>' +
        '</tr>'
      ).join('');
    }
  } catch (e) { /* ignore */ }
}

// ========== opencode free models ==========
// Last-seen zen config; the proxy pool fields live on the Proxy pool page now,
// so saves must carry the cached values instead of removed form inputs.
let ocCfgCache = {};

async function loadOcConfig() {
  try {
    await loadProxyListCache();
    const d = await api('GET', '/opencode/config');
    const c = d.data;
    ocCfgCache = c;
    const ks = c.keyStates || [];
    // 标题下统计：key · 可用（路由启用）· 冷却中（与 Cline 页摘要同款）
    const sum = _('ocSummary');
    if (sum) {
      const routed = ks.filter(k => k.routingEnabled !== false).length;
      const cooling = ks.filter(k => k.cooling).length;
      sum.textContent = ks.length + ' ' + T('keys') + ' · ' + routed + ' ' + T('active') + ' · ' + cooling + ' ' + T('cooling');
    }
    renderOcKeyStates(ks);
    _('ocBaseURL').value = c.baseURL || '';
    _('ocMaxConc').value = c.maxConcurrency || 8;
    _('ocRetries').value = c.retries || 3;
    _('ocCompactAuto').value = String(c.compaction ? c.compaction.auto : true);
    _('ocCompactBuffer').value = c.compaction ? c.compaction.buffer : 20000;
    _('ocKeepTokens').value = c.compaction ? c.compaction.keepTokens : 8000;
    _('ocSummaryModel').value = c.compaction ? (c.compaction.summaryModel || '') : '';
    _('ocMaxSummary').value = c.compaction ? c.compaction.maxSummary : 4096;
  } catch (e) { /* ignore */ }
}

async function saveOcConfig() {
  // key 列表不随本表单提交（增删在 key 表的添加凭据/删除按钮里），避免
  // 旧 textarea 快照覆盖面板当前的 key 池。
  const body = {
    baseURL: _('ocBaseURL').value.trim(),
    proxies: ocCfgCache.proxies || [],
    proxyStrategy: ocCfgCache.proxyStrategy || 'round_robin',
    maxConcurrency: parseInt(_('ocMaxConc').value) || 8,
    retries: parseInt(_('ocRetries').value) || 3,
    compaction: {
      auto: _('ocCompactAuto').value === 'true',
      buffer: parseInt(_('ocCompactBuffer').value) || 20000,
      keepTokens: parseInt(_('ocKeepTokens').value) || 8000,
      summaryModel: _('ocSummaryModel').value.trim(),
      maxSummary: parseInt(_('ocMaxSummary').value) || 4096
    }
  };
  try {
    const d = await api('POST', '/opencode/config/update', body);
    toast('OpenCode config saved', 'success');
    loadOcConfig();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// per-key 状态表：key 掩码 / 用量 / 会话 / 冷却（含预计恢复时刻）/ 测试+删除。
// Test 与 cline 账号的同语义：真实探测，成功即复位该 key 的冷却。
// Proxy binding 列与 Pool（路由启用）列均为暂存编辑：改动行高亮，由
// "保存"按钮统一提交（隔离模式语义见 Proxy pool 页）。铸造跟随路由启用，
// 不再有独立的铸造开关列。
let keySnapshot = {};
function renderOcKeyStates(ks) {
  const tb = _('ocKeysBody');
  if (!tb) return;
  keySnapshot = {};
  tb.innerHTML = ks.length
    ? ks.map(k => {
        const st = k.sessionLive
          ? '<span style="color:var(--accent2)">live</span>'
          : (k.sessionMinted ? '<span style="color:var(--danger)">stale</span>' : '<span style="color:var(--danger)">not minted</span>');
        const cool = k.cooling
          ? '<span style="color:var(--danger)">' + T('cooling') + (k.cooldownUntil ? T(' · until ') + esc(fmtWhen(k.cooldownUntil)) : '') + '</span>'
          : '<span style="color:var(--text2)">-</span>';
        // omitempty 下未绑定 key 没有这两个键，归一化为 ''
        const pMain = k.proxyMain || '';
        const pBackup = k.proxyBackup || '';
        // 辅代理开关关闭 → 辅槽只读跟随主槽（key 表与账号表同规则）
        const backupLocked = !backupProxyOn || pMain === '' || pMain === EGRESS_DIRECT;
        const backupVal = backupLocked ? pMain : pBackup;
        const bind = '<td style="white-space:nowrap">' +
          egComboHTML('key', k.index, 'main', pMain, T('Main exit — used whenever available')) + ' ' +
          egComboHTML('key', k.index, 'backup', backupVal, T('Backup exit — used when main is cooling/removed'), backupLocked) +
          (k.proxyStale ? ' <span title="' + esc(T('Bound proxy is no longer in the proxy pool — the key is skipped until fixed')) + '" style="color:var(--danger)">⚠</span>' : '') +
          '</td>';
        // Pool（路由启用）列：勾选后该 key 才参与请求轮转；铸造跟随路由，
        // 路由关闭的 key 也不再被自动铸造。匿名 public key 恒参与路由。
        const routed = k.routingEnabled !== false;
        keySnapshot[k.index] = { main: pMain, backup: backupVal, routing: routed, pub: k.keyMask === 'public (no key)' };
        const routing = '<td style="text-align:center">' + (k.keyMask === 'public (no key)'
          ? '<span style="color:var(--text3)">-</span>'
          : '<input type="checkbox" data-zr="' + k.index + '"' + (routed ? ' checked' : '') + ' title="' + esc(T('Participates in request routing')) + '">') + '</td>';
        // 操作列：测试（真实探测）+ 删除（红色，从 key 池移除该 key）
        const actions = '<td style="white-space:nowrap">' + (k.keyMask === 'public (no key)'
          ? '<span style="color:var(--text3)">-</span>'
          : '<button class="btn btn-sm" data-zk="' + k.index + '">Test</button> ' +
            '<button class="btn btn-sm btn-danger" data-zdel="' + k.index + '" title="Remove this key from the pool">Delete</button>') + '</td>';
        return '<tr draggable="true" data-drag-id="' + k.index + '"><td>#' + (k.index + 1) + (k.current ? ' <span style="color:var(--accent)" title="next in rotation">●</span>' : '') + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + esc(k.keyMask) + '</td>' +
          '<td>' + (k.usage || 0) + '</td>' +
          '<td>' + st + '</td>' +
          '<td>' + cool + '</td>' +
          bind +
          actions +
          routing + '</tr>';
      }).join('')
    : '<tr><td colspan="8" class="empty">No opencode keys configured</td></tr>';
  tb.onclick = e => {
    const b = e.target.closest('button[data-zk]');
    if (b) { testZenKey(parseInt(b.dataset.zk, 10), b); return; }
    const d = e.target.closest('button[data-zdel]');
    if (d) deleteZenKey(parseInt(d.dataset.zdel, 10), d);
  };
  tb.onchange = e => {
    const r = e.target.closest('input[data-zr]');
    if (r) markRowDirty(r.closest('tr'), 'key');
  };
}

function stageAllKeysRouting(enabled) {
  document.querySelectorAll('#ocKeysBody input[data-zr]').forEach(cb => {
    const snap = keySnapshot[parseInt(cb.dataset.zr, 10)];
    if (snap && snap.routing === enabled) return;
    cb.checked = enabled;
    markRowDirty(cb.closest('tr'), 'key');
  });
}
async function saveKeyEdits() {
  const rows = [...document.querySelectorAll('#ocKeysBody tr')].filter(r => r.querySelector('.eg-input'));
  let changed = 0, failed = 0;
  for (const row of rows) {
    const mainInput = row.querySelector('.eg-input[data-eg="key"][data-slot="main"]');
    const idx = parseInt(mainInput.dataset.egid, 10);
    const main = mainInput.dataset.value || '';
    const backup = row.querySelector('.eg-input[data-eg="key"][data-slot="backup"]').dataset.value || '';
    const rcb = row.querySelector('input[data-zr]');
    const snap = keySnapshot[idx] || { main: '', backup: '', routing: true };
    try {
      if (main !== snap.main || backup !== snap.backup) {
        await api('POST', '/opencode/keys/proxy', { index: idx, main: main, backup: backup });
      }
      if (rcb && rcb.checked !== snap.routing) {
        await api('POST', '/opencode/keys/routing', { index: idx, enabled: rcb.checked });
      }
      if (main !== snap.main || backup !== snap.backup || (rcb && rcb.checked !== snap.routing)) changed++;
      row.classList.remove('tr-dirty');
    } catch (e) { failed++; }
  }
  clearDirtyMarks('ocKeysBody', 'ocKeysSaveBtn');
  if (failed) toast(T('Save failed for ') + failed + T(' account(s)'), 'error');
  else if (!changed) toast(T('No changes to save'), 'info');
  else toast(T('Saved ') + changed + T(' account change(s)'), 'success');
  loadOcConfig();
}

// 复位：丢弃 key 表的全部暂存修改，按上次保存的状态重绘
function resetKeyEdits() {
  clearDirtyMarks('ocKeysBody', 'ocKeysSaveBtn');
  loadOcConfig();
}

// 删除单个 key：从 key 池移除并按索引刷新（索引会前移，直接重拉）。
async function deleteZenKey(index, btn) {
  if (!confirm(T('Delete this key?'))) return;
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  try {
    await api('POST', '/opencode/keys/delete', { index: index });
    toast(T('Key #') + (index + 1) + T(' (deleted)'), 'success');
  } catch (e) {
    toast(T('Delete failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOcConfig();
  }
}

// 批量测试：等价于对每个 key 点一次 Test（真实探测，成功即复位冷却）。
// 匿名 public key 无凭据可测，跳过。逐个串行避免打爆上游，按钮显示进度。
async function batchTestKeys() {
  const btn = _('ocTestBtn');
  const idxs = Object.keys(keySnapshot).map(Number)
    .filter(i => !keySnapshot[i].pub)
    .sort((a, b) => a - b);
  if (!idxs.length) { toast(T('No keys to test'), 'info'); return; }
  const original = btn ? btn.innerHTML : '';
  if (btn) btn.disabled = true;
  let ok = 0, cool = 0, err = 0;
  try {
    for (let i = 0; i < idxs.length; i++) {
      if (btn) btn.innerHTML = '<span class="loading"></span>' + (i + 1) + '/' + idxs.length;
      try {
        const d = await api('POST', '/zen/keys/test', {
          index: idxs[i],
          model: (_('ocProbeModel') ? _('ocProbeModel').value : '')
        });
        const r = d.data || {};
        if (r.status === 'active') ok++;
        else if (r.status === 'cooldown') cool++;
        else err++;
      } catch (e) { err++; }
    }
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
  }
  toast(T('Batch test done: ') + idxs.length + T(' tested, ') + ok + T(' ok') +
    (cool ? T(', ') + cool + T(' cooling') : '') + (err ? T(', ') + err + T(' error') : ''),
    err ? 'warning' : 'success', 6000);
  loadOcConfig();
}

// ========== 添加凭据弹窗（key 逐行填入文本域，合并进当前 key 池） ==========
function openZenKeyAddDialog() {
  _('zenKeyAddInput').value = '';
  _('zenKeyAddDialog').showModal();
}
function closeZenKeyAddDialog() { _('zenKeyAddDialog').close(); }
async function zenKeyAddFromDialog() {
  const lines = _('zenKeyAddInput').value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { toast(T('Enter at least one key'), 'error'); return; }
  const cur = (ocCfgCache.keys && ocCfgCache.keys.length) ? ocCfgCache.keys : [ocCfgCache.key || 'public'];
  const fresh = lines.filter(l => !cur.includes(l));
  if (!fresh.length) { toast(T('No new keys (all already in the pool)'), 'info'); return; }
  const btn = _('zenKeyAddSubmit');
  btn.disabled = true;
  try {
    await api('POST', '/opencode/config/update', { keys: cur.concat(fresh) });
    toast(T('Added ') + fresh.length + T(' key(s)'), 'success');
    closeZenKeyAddDialog();
    loadOcConfig();
  } catch (e) {
    toast(T('Save failed: ') + e.message, 'error');
  } finally {
    btn.disabled = false;
  }
}

async function testZenKey(index, btn) {
  const original = btn ? btn.innerHTML : '';
  if (btn) { btn.disabled = true; btn.innerHTML = '<span class="loading"></span>Testing'; }
  try {
    const d = await api('POST', '/zen/keys/test', {
      index: index,
      // 下拉里的探测模型；空串 = 后端自动选（big-pickle → live → 种子）
      model: (_('ocProbeModel') ? _('ocProbeModel').value : '')
    });
    const r = d.data || {};
    const label = { active: T('OK'), cooldown: T('Cooldown'), error: T('Error') }[r.status] || r.status;
    let msg = T('Key #') + (index + 1) + T(' (') + (r.keyMask || '') + T(') — ') + label;
    if (r.model) msg += T(' via ') + r.model;
    if (r.latencyMs != null) msg += ' (' + r.latencyMs + 'ms)';
    if (r.status === 'active') msg += T(' — cooldown cleared');
    if (r.cooldownUntil) msg += T('\nEstimated recovery: ') + fmtWhen(r.cooldownUntil) + (r.remaining ? T(' (remaining ') + r.remaining + T(')') : '');
    if (r.reason && r.reason !== 'ok') msg += '\n' + r.reason;
    const type = r.status === 'active' ? 'success' : (r.status === 'cooldown' ? 'warning' : 'error');
    toast(msg, type, 6000);
  } catch (e) {
    toast(T('Test failed: ') + e.message, 'error');
  } finally {
    if (btn) { btn.disabled = false; btn.innerHTML = original; }
    loadOcConfig();  // 冷却被清除/新设置，立即刷新状态表
  }
}

// ========== Proxy pool ==========
async function loadProxyPool() {
  try {
    const [oc, cfg] = await Promise.all([api('GET', '/opencode/config'), api('GET', '/config')]);
    const c = oc.data;
    proxyListCache = (c.proxies || []).slice();
    syncProxyHealthCache((c.runtime || {}).proxyHealth);
    backupProxyOn = c.backupProxyEnabled === true;
    _('ppProxies').value = (c.proxies || []).join('\n');
    _('ppStrategy').value = c.proxyStrategy || 'round_robin';
    _('ppCline').value = String(!!cfg.data.clineUseProxies);
    _('ppIsolation').value = String(c.proxyIsolation !== false);
    _('ppBackupEnabled').checked = c.backupProxyEnabled === true;
    if (c.proxyIsolationEnvLocked) {
      _('ppIsolation').disabled = true;
      _('ppIsolationHint').innerHTML = '<span style="color:var(--warning,#eab308)">PROXY_ISOLATION=true is set — isolation is forced on. Unset the env var (or set it to false) to control isolation from the panel.</span>';
    } else {
      _('ppIsolation').disabled = false;
      _('ppIsolationHint').textContent = '';
    }
    _('ppZen').value = String(c.zenUseProxies !== false);
    _('ppWb').value = String(c.workbuddyUseProxies !== false);
    renderProxyHealth((c.runtime || {}).proxyHealth || []);
  } catch (e) { /* ignore */ }
}

// 每代理在线率表：纯被动统计（真实请求的出口成败），编辑代理池即清零。
// 低在线率标红是展示信号，不改任何路由行为——换不换代理由人决定。
const PROXY_HEALTH_LOW_RATE = 0.8, PROXY_HEALTH_MIN_SAMPLES = 10;
function renderProxyHealth(rows) {
  const tb = _('ppHealthBody');
  if (!tb) return;
  tb.innerHTML = rows.length
    ? rows.map(r => {
        const pct = rate => (rate === null || rate === undefined) ? null : Math.round(rate * 100) + '%';
        const rateCell = v => v === null
          ? '<span style="color:var(--text3)">-</span>'
          : '<span style="color:' + (v.low ? 'var(--danger)' : 'var(--text)') + ';font-weight:' + (v.low ? '600' : '400') + '">' + v.text + '</span>';
        const cum = pct(r.rate);
        const rec = pct(r.recentRate);
        const low = r.recentTotal >= PROXY_HEALTH_MIN_SAMPLES && r.recentRate !== null && r.recentRate !== undefined && r.recentRate < PROXY_HEALTH_LOW_RATE;
        const failTitle = r.lastErr ? ' title="' + esc(r.lastErr) + '"' : '';
        return '<tr>' +
          '<td class="mono" style="font-size:11px">' + esc(egLabel(r.proxy)) + '</td>' +
          '<td style="text-align:center">' + (r.attempts || 0) + '</td>' +
          '<td style="text-align:center">' + rateCell(cum === null ? null : { text: cum, low: false }) + '</td>' +
          '<td style="text-align:center">' + rateCell(rec === null ? null : { text: rec, low: low }) + '</td>' +
          '<td class="mono" style="font-size:11px">' + (r.lastOk ? fmtWhen(r.lastOk) : '<span style="color:var(--text3)">-</span>') + '</td>' +
          '<td class="mono" style="font-size:11px"' + failTitle + '>' + (r.lastFail ? fmtWhen(r.lastFail) : '<span style="color:var(--text3)">-</span>') + '</td>' +
          '<td>' + (r.cooling ? '<span style="color:var(--warning,#eab308)">' + T('cooldown until ') + fmtWhen(r.coolingUntil) + '</span>' : '<span style="color:var(--text3)">-</span>') + '</td>' +
        '</tr>';
      }).join('')
    : '<tr><td colspan="7" class="empty">No proxies configured</td></tr>';
}

async function saveProxyPool() {
  const proxies = _('ppProxies').value.split('\n').map(s => s.trim()).filter(Boolean);
  const PROXY_RE = /^(https?|socks5h?|socks):\/\/[^\s]+:\d+/;
  const bad = proxies.find(p => !PROXY_RE.test(p));
  if (bad) { toast(T('Invalid proxy format: ') + bad + ' (need http(s)://host:port or socks5://host:port)', 'error'); return; }
  try {
    await api('POST', '/opencode/config/update', { proxies: proxies, proxyStrategy: _('ppStrategy').value });
    _('ppSaveResult').innerHTML = '<div style="color:var(--accent2);font-size:12px">' + T('Saved ') + proxies.length + T(' proxies') + '</div>';
    setTimeout(() => _('ppSaveResult').innerHTML = '', 5000);
    toast('Proxy pool saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// saveGlobalPolicy 统一保存三个平台的全局走池开关：一次点击依次下发
// Cline（/config/update）与 OpenCode / WorkBuddy（/opencode/config/update）。
async function saveGlobalPolicy() {
  try {
    await api('POST', '/config/update', { clineUseProxies: _('ppCline').value === 'true' });
    await api('POST', '/opencode/config/update', { zenUseProxies: _('ppZen').value === 'true', workbuddyUseProxies: _('ppWb').value === 'true' });
    toast('Global policy saved', 'success');
    loadProxyPool();
  } catch (e) { toast(T('Save failed: ') + e.message, 'error'); }
}

// saveIsolationOptions 保存代理隔离区的两项：隔离模式 + 辅代理开关。两字段同在
// zen 配置但分两次请求下发——PROXY_ISOLATION=true 锁定面板时后端会整体拒绝携带
// proxyIsolation 的请求，分开才能让辅代理开关在锁定态照常保存（锁定态下拉框
// disabled，跳过第一笔）。辅代理开关联动绑定表重绘，保存失败回滚勾选。
async function saveIsolationOptions() {
  const iso = _('ppIsolation');
  if (!iso.disabled) {
    try {
      await api('POST', '/opencode/config/update', { proxyIsolation: iso.value === 'true' });
    } catch (e) { toast(T('Save failed: ') + e.message, 'error'); return; }
  }
  const cb = _('ppBackupEnabled');
  try {
    await api('POST', '/opencode/config/update', { backupProxyEnabled: cb.checked });
    backupProxyOn = cb.checked;
    toast('Proxy isolation settings saved', 'success');
    loadProxyPool();
    refreshBindingTables();
  } catch (e) {
    cb.checked = !cb.checked;  // 保存失败回滚勾选
    toast(T('Save failed: ') + e.message, 'error');
  }
}

// refreshBindingTables 按当前 backupProxyOn 重绘绑定表：只刷新已经渲染过行的
// 表（标签页是惰性加载的，没打开过的保持未加载）。行内的暂存改动会被丢弃，
// 与"复位"同语义 —— 开关是全局设置，重绘比就地改 DOM 更不容易出现半更新。
function refreshBindingTables() {
  if (_('accountTableBody') && _('accountTableBody').querySelector('.eg-input')) loadAccounts();
  if (_('ocKeysBody') && _('ocKeysBody').querySelector('.eg-input')) loadOcConfig();
  if (_('wbAccountsBody') && _('wbAccountsBody').querySelector('.eg-input')) loadWorkbuddyAccounts();
}

// loadOcModels 只维护探测模型下拉（模型列表本体已并入网关设置的可用模型区）。
async function loadOcModels() {
  try {
    const d = await api('GET', '/opencode/models');
    const models = d.data.models || [];
    const sel = _('ocProbeModel');
    if (sel) {
      const prev = sel.value;
      sel.innerHTML = '<option value="">' + T('auto — big-pickle first, then live models') + '</option>' +
        models.map(m => '<option value="' + esc(m.id) + '">' + esc(m.id) + (m.source === 'live' ? '' : T(' (seed)')) + '</option>').join('');
      if (Array.from(sel.options).some(o => o.value === prev)) sel.value = prev;
    }
  } catch (e) { /* ignore */ }
}

// ========== Live session IDs (zen FreeTier gate) ==========
// 未 mint 的 key 必 403；表格让"哪些 key 还是空会话"一眼可见，按钮给手动补收口。
let ocSessPoll = null;

async function loadOcSessions() {
  try {
    const d = await api('GET', '/opencode/sessions');
    renderOcSessions(d.data);
    return d.data;
  } catch (e) { return null; }
}

function renderOcSessions(s) {
  const rows = s.sessions || [];
  _('ocSessSummary').innerHTML = rows.length
    ? '<span style="color:' + (s.liveCount === s.total ? 'var(--accent2)' : 'var(--danger)') + '">' +
      s.liveCount + '/' + s.total + ' live</span>'
    : '';
  // 刷新节奏与并发写进提示行：用户据此判断"自动维护是否够用"，以及
  // 调 ZEN_HARVEST_INTERVAL_HOURS / ZEN_HARVEST_CONCURRENCY 要不要改。
  // 本区域是 innerHTML 整段重写，静态词条不生效——每段直接走 T() 字典。
  const h = _('ocSessHint');
  if (h) {
    let html = T('The free tier only accepts session IDs the upstream has actually seen, minted by the opencode CLI. A key without a live session <b>always</b> fails with 403.');
    if (s.harvestEnabled) {
      html += ' ' + T('Auto-refresh every {h}h (kept below the 5h quota window), minting {n} key(s) at a time')
          .replace('{h}', s.intervalHours || 4)
          .replace('{n}', s.concurrency || 1);
      if ((s.concurrency || 1) === 1) html += T(' (one after another — safest on small instances, the CLI is CPU/RAM hungry)');
      html += T('.');
    } else {
      html += ' <b>' + T('Harvester unavailable') + '</b>' + T(' — no opencode CLI in this container (ZEN_HARVEST_BIN).');
    }
    h.innerHTML = html;
  }
  // CLI 不在时后端会以 400 拒绝 mint，按钮先禁用，避免点了才发现。
  const usable = !!s.harvestEnabled;
  ['ocSessBtnMissing', 'ocSessBtnForce'].forEach(id => {
    const b = _(id);
    if (b) { b.disabled = !usable; b.title = usable ? '' : 'opencode CLI not available in this container'; }
  });
  _('ocSessBody').innerHTML = rows.length
    ? rows.map(k => {
        const state = k.noKey
          ? '<span style="color:var(--text2)">no key</span>'
          : (k.live
              ? '<span style="color:var(--accent2)">live</span>'
              : (k.minted ? '<span style="color:var(--danger)">stale</span>' : '<span style="color:var(--danger)">not minted</span>'));
        return '<tr><td>#' + (k.index + 1) + '</td>' +
          '<td style="font-family:monospace;font-size:11px">' + (k.session ? esc(k.session) + '…' : '-') + '</td>' +
          '<td>' + state + '</td>' +
          '<td style="font-size:12px">' + (k.harvested ? esc(fmtWhen(k.harvested)) : '-') + '</td></tr>';
      }).join('')
    : '<tr><td colspan="4" class="empty">No opencode keys configured</td></tr>';

  const j = s.job;
  if (!j) { _('ocSessTimer').textContent = ''; return; }
  if (j.running) {
    _('ocSessTimer').innerHTML = '<span style="color:var(--accent)">minting ' + (j.done || 0) + '/' + (j.total || 0) + '…</span>';
    renderOcMintResults(j.results || [], true);
  } else {
    _('ocSessTimer').textContent = T('last mint ') + fmtWhen(j.startedAt) + (j.force ? T(' (force)') : '');
    renderOcMintResults(j.results || [], false);
  }
}

function renderOcMintResults(results, running) {
  if (!results.length) { _('ocSessResult').innerHTML = ''; return; }
  const ok = results.filter(r => r.ok).length;
  const skipped = results.filter(r => r.skipped).length;
  const fail = results.filter(r => r.done && !r.ok && !r.skipped).length;
  const pending = results.filter(r => !r.done).length;
  const head = running
    ? '<span style="color:var(--accent)">' + T('Running — ') + ok + T(' minted, ') + fail + T(' failed, ') + pending + T(' pending') + '</span>'
    : (fail
        ? '<span style="color:var(--danger)">' + T('Done — ') + ok + T(' minted, ') + fail + T(' failed') + (skipped ? T(', ') + skipped + T(' skipped (already live)') : '') + '</span>'
        : '<span style="color:var(--accent2)">' + T('Done — ') + ok + T(' minted') + (skipped ? ', ' + skipped + T(' already live') : '') + '</span>');
  const details = results.filter(r => r.done && !r.ok && !r.skipped)
    .map(r => '#' + (r.index + 1) + ': ' + esc(r.error || 'failed')).join(' | ');
  _('ocSessResult').innerHTML = '<div style="font-size:12px">' + head + '</div>' +
    (details ? '<div class="hint" style="margin-top:4px">' + details + '</div>' : '');
}

async function mintZenSessions(force) {
  try {
    await api('POST', '/opencode/sessions/mint', { force: force });
    toast(force ? 'Force minting all sessions…' : 'Minting missing sessions…', 'success');
    // 任务在后端跑（串行 mint 时一批是 key 数 × 每 key 预算），按 2s 轮询进度。
    // 上限按后端批次预算推算：并发默认为 1 后 11 个 key 的批次上限可达几十分钟，
    // 写死 450 次（15 分钟）会让轮询提前停掉、进度条卡住不再更新。
    if (ocSessPoll) { clearInterval(ocSessPoll); ocSessPoll = null; }
    let ticks = 0;
    const s0 = await loadOcSessions();
    // 提前返回前必须把 ocSessPoll 置空：可见标签页的 20s 轮询用 !ocSessPoll
    // 判断"是否已有轮询在跑"，留一个已 clear 的非空句柄会让它永久停摆。
    if (!s0 || !s0.job || !s0.job.running) { ocSessPoll = null; return; }
    const j0 = s0.job;
    const workers = Math.max(1, s0.concurrency || 1);
    const perKey = s0.keyTimeoutSeconds || 150;
    // 下限 15 分钟与后端 harvestBatchTimeout 的下限对齐：key 少时后端照样跑满
    // 15 分钟，而 ceil(total/workers)×perKey 只有几分钟，不夹下限轮询会在后端
    // 放弃之前就停掉（之后只剩 20s 的可见标签页轮询，进度更新变粗）。
    const batchSeconds = Math.max(900, Math.ceil((j0.total || 1) / workers) * perKey + 120);
    const maxTicks = Math.min(1800, Math.ceil(batchSeconds / 2) + 30);
    ocSessPoll = setInterval(async () => {
      const s = await loadOcSessions();
      if (!s || !s.job || !s.job.running || ++ticks >= maxTicks) {
        clearInterval(ocSessPoll); ocSessPoll = null; loadOcConfig();
      }
    }, 2000);
  } catch (e) { toast(T('Mint failed: ') + e.message, 'error'); }
}

// ========== Combos (alias models) ==========
const comboModels = { cline: [], zen: [], workbuddy: [] };

async function fillComboModels() {
  const platform = _('comboPlatform').value;
  const sel = _('comboTarget');
  try {
    if (!comboModels[platform].length) {
      if (platform === 'cline') {
        const d = await api('GET', '/models');
        comboModels.cline = (d.data.models || []).map(m => m.id).filter(Boolean);
      } else if (platform === 'zen') {
        const d = await api('GET', '/opencode/models');
        comboModels.zen = (d.data.models || []).map(m => m.id).filter(Boolean);
      } else if (platform === 'workbuddy') {
        const all = await fetchWorkbuddyModels();
        comboModels.workbuddy = all.filter(wbModelMatchesFilter)
          .sort((a, b) => wbRate(a) - wbRate(b))
          .map(m => m.id).filter(Boolean);
      }
    }
    if (!comboModels[platform].length) { sel.innerHTML = '<option value="">No models on this platform</option>'; return; }
    sel.innerHTML = comboModels[platform].map(id => '<option value="' + esc(id) + '">' + esc(id) + '</option>').join('');
  } catch (e) { sel.innerHTML = '<option value="">Failed to load model list</option>'; }
}

async function loadCombos() {
  try {
    const d = await api('GET', '/combos');
    const combos = d.data.combos || [];
    if (!combos.length) { _('combosList').innerHTML = '<div class="hint">No combos yet — create one with the form above.</div>'; return; }
    _('combosList').innerHTML = '<div class="table-wrap"><table><thead><tr><th style="text-align:left">Alias ID</th><th>Platform</th><th style="text-align:left">Target model</th><th>Proxy</th><th>Created</th><th>Actions</th></tr></thead><tbody>' +
      combos.map(c => '<tr>' +
        '<td style="text-align:left;font-family:monospace;font-weight:600">' + esc(c.id) + '</td>' +
        '<td><span class="model-tag">' + esc(c.platform === 'zen' ? 'opencode' : c.platform) + '</span></td>' +
        '<td style="text-align:left;font-family:monospace">' + esc(c.target) + '</td>' +
        '<td>' + (c.useProxies ? '<span class="model-tag" style="color:var(--accent2)">socks5 pool</span>' : '-') + '</td>' +
        '<td style="font-size:11px">' + (c.createdAt ? new Date(c.createdAt).toLocaleString('en-US') : '-') + '</td>' +
        '<td><button class="btn btn-sm btn-danger" data-delcombo="' + esc(c.id) + '">Delete</button></td>' +
      '</tr>').join('') +
      '</tbody></table></div>';
    _('combosList').onclick = e => {
      const b = e.target.closest('button[data-delcombo]');
      if (b) deleteCombo(b.dataset.delcombo);
    };
  } catch (e) { _('combosList').textContent = T('Failed to load: ') + e.message; }
}

async function createCombo() {
  const id = _('comboId').value.trim();
  const platform = _('comboPlatform').value;
  const target = _('comboTarget').value;
  if (!id) { toast('Enter an alias ID', 'error'); return; }
  if (!target) { toast('Select a target model', 'error'); return; }
  try {
    await api('POST', '/combos/create', { id, platform, target, useProxies: _('comboUseProxies').checked });
    toast(T('Combo created: ') + id + ' → ' + target, 'success');
    _('comboId').value = '';
    _('comboUseProxies').checked = false;
    loadCombos();
  } catch (e) { toast(T('Create failed: ') + e.message, 'error'); }
}

async function deleteCombo(id) {
  if (!confirm(T('Delete combo ') + id + T('? Clients using this alias will no longer be able to call it.'))) return;
  try {
    await api('POST', '/combos/delete', { id });
    toast('Combo deleted', 'success');
    loadCombos();
  } catch (e) { toast(T('Delete failed: ') + e.message, 'error'); }
}

async function loadOcStats() {
  try {
    const d = await api('GET', '/opencode/stats');
    const t = d.data.today || {}, s = d.data.total || {};
    _('ocStatsBox').innerHTML = '<table><thead><tr><th style="text-align:left"></th><th>Requests</th><th>Input tokens</th><th>Output tokens</th><th>Compaction</th><th>Rate-limited</th></tr></thead><tbody>' +
      '<tr><td style="text-align:left">Today</td><td>' + (t.requests || 0) + '</td><td>' + (t.promptTokens || 0) + '</td><td>' + (t.completionTokens || 0) + '</td><td>' + (t.compaction || 0) + '</td><td>' + (t.rateLimited || 0) + '</td></tr>' +
      '<tr><td style="text-align:left">Total</td><td>' + (s.requests || 0) + '</td><td>' + (s.promptTokens || 0) + '</td><td>' + (s.completionTokens || 0) + '</td><td>' + (s.compaction || 0) + '</td><td>' + (s.rateLimited || 0) + '</td></tr>' +
      '</tbody></table>';
    const bm = t.byModel || {};
    const rows = Object.keys(bm).map(k => '<tr><td style="text-align:left;font-family:monospace">' + esc(k) + '</td><td>' + bm[k].requests + '</td><td>' + bm[k].promptTokens + '</td><td>' + bm[k].completionTokens + '</td></tr>').join('');
    _('ocModelStatsBox').innerHTML = '<div style="font-size:13px;font-weight:600;margin-bottom:6px">Per-model breakdown (today)</div>' +
      '<div class="table-wrap"><table><thead><tr><th style="text-align:left">Model</th><th>Requests</th><th>Input tokens</th><th>Output tokens</th></tr></thead><tbody>' +
      (rows || '<tr><td colspan="4" style="text-align:left;color:var(--text2)">No data</td></tr>') + '</tbody></table></div>';
  } catch (e) { /* ignore */ }
}

// ========== Init ==========
loadStats();
loadAccounts();
loadKeys();
loadModels();
loadConfig();
restoreTab();
// 拖拽排序（Account pool 三菜单）：submit 即时落库，reload 重拉对齐派生状态
// （ocKeysBody 的 index 暂存协议、Cline 的轮转游标、wb 的 order 持久化）。
enableRowDragReorder('accountTableBody', ids => api('POST', '/accounts/reorder', { order: ids }), loadAccounts);
enableRowDragReorder('ocKeysBody', ids => api('POST', '/opencode/keys/reorder', { order: ids.map(Number) }), loadOcConfig);
enableRowDragReorder('wbAccountsBody', ids => api('POST', '/workbuddy/reorder', { uids: ids }), loadWorkbuddyAccounts);
// 平台概览只在仪表盘可见时轮询（loadStats 现在打三个接口，隐藏页轮询纯浪费）
setInterval(() => { if (_('tab-dashboard').style.display !== 'none') loadStats(); }, 10000);
setInterval(() => { loadOcStats(); }, 15000);
// live 会话状态：只在 opencode 页可见时轮询（与日志页同样的省流约定）。
// mint 任务进行中由 mintZenSessions 自己的 2s 轮询接管。
setInterval(() => {
  if (_('tab-opencode').style.display !== 'none' && !ocSessPoll) loadOcSessions();
}, 20000);
setInterval(() => { if (logsAuto && _('tab-logs').style.display !== 'none') loadLogs(); }, 8000);
</script>
</body>
</html>`
