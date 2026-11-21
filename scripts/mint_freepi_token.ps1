# mint_freepi_token.ps1 — 铸造 FreePI (freepi.ai) JWT（GitHub 设备码流程）
# 流程移植自上游 pingmike2/freepi-magpie-plugin 的 index.mjs authorize：
#   device → consent(v4，实测为 /auth/token 前置) → 轮询 token → /me 取 handle
# 产物即面板「FreePI → 添加凭据」的粘贴行：JWT-----昵称（昵称可省略），
# 且自动复制进剪贴板——误关窗口也不会丢。
#
# 用法（cmd）：
#   powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0mint_freepi_token.ps1" [参数]
#   或直接运行 mint_freepi_token.cmd（结束后窗口停在命令行，手动关闭）
# 参数：
#   -Nickname freepi-main                 面板昵称
#   -Proxy socks5h://127.0.0.1:10086      api.freepi.ai 直连不通时用
#   -Open                                 用系统默认浏览器打开 GitHub 设备页
#   -Browser "C:\...\chrome.exe"          指定浏览器 exe
#   -ProfileDir "Profile 1"               Chrome/Edge 配置目录——多账号各用各的 GitHub 登录态
#   -NoMe                                 跳过 /me 校验
#   -NoClip                               不写剪贴板
# 多账号：每个 GitHub 账号跑一次，在其已登录的浏览器/配置里完成授权。
# 误操作防护：Ctrl+C 被当作普通按键忽略（复制链接也不会中断）；放弃请关窗口 ✕。

param(
  [string]$Nickname = '',
  [string]$Proxy = '',
  [string]$TimeZone = 'Asia/Shanghai',
  [switch]$Open,
  [string]$Browser = '',
  [string]$ProfileDir = '',
  [switch]$NoMe,
  [switch]$NoClip
)
$ErrorActionPreference = 'Stop'
$BASE = 'https://api.freepi.ai'

# Ctrl+C 免疫：作为普通输入进入缓冲、无人读取即忽略；finally 里恢复并清缓冲
try { [Console]::TreatControlCAsInput = $true } catch {}

# 统一 curl 调用：返回 {ok,status,body}。
# JSON body 一律走临时文件 -d @file——PS5.1 向 curl.exe 内联传含双引号的参数
# 会被 Windows CRT 吞引号（实测 body 变 {a:b} 被上游 400 body is not JSON）。
$bodyFile = Join-Path $env:TEMP 'freepi_mint_body.json'
function Invoke-CurlJson {
  param([string]$Method, [string]$Url, [string]$Body = '', [string[]]$Headers = @())
  $a = @('-s','--max-time','30','-X',$Method,$Url,'-w',"`n%{http_code}")
  if ($Proxy) { $a += @('-x', $Proxy) }
  if ($Body) {
    [IO.File]::WriteAllText($bodyFile, $Body)
    $a += @('-H','content-type: application/json','-d',"@$bodyFile")
  }
  foreach ($h in $Headers) { $a += @('-H',$h) }
  $raw = & curl.exe @a
  if ($Body) { Remove-Item $bodyFile -ErrorAction SilentlyContinue }
  if ($LASTEXITCODE -ne 0) { return [pscustomobject]@{ ok=$false; status=0; body='' } }
  $txt = ($raw -join "`n")
  $i = $txt.LastIndexOf("`n")
  $status = 0
  if ($i -ge 0) { [void][int]::TryParse($txt.Substring($i+1).Trim(), [ref]$status); $txt = $txt.Substring(0,$i) }
  [pscustomobject]@{ ok=$true; status=$status; body=$txt }
}

try {

# ① 申请设备码会话（5xx/网络失败自动重试 3 次——上游服务端偶发 500 实测过）
Write-Host '[1/4] 向 FreePI 申请设备码会话…'
$dev = $null
for ($try = 1; $try -le 3; $try++) {
  $dev = Invoke-CurlJson -Method POST -Url "$BASE/auth/github/device" -Body '{}'
  if ($dev.ok -and $dev.status -lt 400) { break }
  if ($try -lt 3) { Write-Host "  第 $try 次失败 HTTP $($dev.status) $($dev.body)——5s 后重试"; Start-Sleep -Seconds 5 }
}
if (-not $dev.ok -or $dev.status -ge 400) {
  Write-Host "  连续三次失败 HTTP $($dev.status) $($dev.body)"
  Write-Host '  建议：等几分钟再试；或加 -Proxy 换出口；或用 curl 手动验证服务端状态。'
  exit 1
}
$d = $dev.body | ConvertFrom-Json
if (-not $d.session_id -or -not $d.user_code) { Write-Host "  响应异常：$($dev.body)"; exit 1 }
$interval = 5
if ($d.interval) { $interval = [int]$d.interval }
if ($interval -lt 3) { $interval = 3 }
$deadline = (Get-Date).AddSeconds($(if ($d.expires_in) { [int]$d.expires_in } else { 900 }))

# ② 同意条款（实测为 /auth/token 前置，缺失返 consent_required；失败不阻断）
$null = Invoke-CurlJson -Method POST -Url "$BASE/auth/consent" -Body (@{ session_id=$d.session_id; consent_version='4'; timezone=$TimeZone; locale='en' } | ConvertTo-Json -Compress)

# ③ 展示设备码，按需打开浏览器
$code = $d.user_code
$url  = if ($d.verification_uri_complete) { $d.verification_uri_complete } else { $d.verification_uri }
Write-Host ''
Write-Host '================ 请在浏览器完成 GitHub 授权 ================'
Write-Host "  激活链接 : $url"
Write-Host "  设备码   : $code   （页面应已预填；为空就手动输入）"
Write-Host '  → Continue → Authorize，然后回本窗口等结果'
Write-Host '  （复制链接随意，Ctrl+C/选择文本都不会中断脚本）'
Write-Host '==========================================================='
if ($Open) {
  if ($Browser) {
    if ($ProfileDir) { Start-Process -FilePath $Browser -ArgumentList @("--profile-directory=$ProfileDir", $url) | Out-Null }
    else { Start-Process -FilePath $Browser -ArgumentList $url | Out-Null }
  } else { Start-Process $url }
}

# ④ 轮询 /auth/token：pending 继续等（按 retry_after 放慢）；非 403/429 的 4xx 判死
Write-Host '[2/4] 等待你在浏览器完成授权…（放弃请关窗口 ✕；会话 15 分钟有效）'
$pollBody = @{ session_id=$d.session_id; timezone=$TimeZone; locale='en' } | ConvertTo-Json -Compress
$token = $null
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds $interval
  Write-Host ('  … 等待授权（剩余 {0,4}s）' -f [int]($deadline - (Get-Date)).TotalSeconds)
  $r = Invoke-CurlJson -Method POST -Url "$BASE/auth/token" -Body $pollBody
  if (-not $r.ok) { continue }
  if ($r.status -eq 403 -or $r.status -eq 429 -or $r.status -ge 500) { continue }
  if ($r.status -ge 400) { Write-Host "  授权被拒 HTTP $($r.status)：$($r.body)"; exit 1 }
  $b = $null
  try { $b = $r.body | ConvertFrom-Json } catch {}
  if ($b -and $b.status -eq 'pending') {
    if ($b.retry_after) { $interval = [Math]::Max($interval, [int]$b.retry_after) }
    continue
  }
  if ($b -and $b.token) { $token = $b.token; break }
  Write-Host "  意外响应 HTTP $($r.status)：$($r.body)"; exit 1
}
if (-not $token) { Write-Host '会话已过期——重新运行本脚本即可。'; exit 1 }
Write-Host '  已拿到 token。'

# ⑤ /me 校验（免费）并取 GitHub handle
$handle = ''
if (-not $NoMe) {
  Write-Host '[3/4] 校验 token（GET /me，免费）…'
  $me = Invoke-CurlJson -Method GET -Url "$BASE/me" -Headers @("authorization: Bearer $token")
  if ($me.ok -and $me.status -eq 200) {
    try { $handle = ($me.body | ConvertFrom-Json).handle } catch {}
    Write-Host "  /me: $($me.body)"
  } else { Write-Host "  /me 校验失败 HTTP $($me.status) $($me.body)（token 已产出，是否入库自行斟酌）" }
} else { Write-Host '[3/4] 已按要求跳过 /me 校验' }

# ⑥ 输出面板粘贴行（本地解码 JWT exp 显示有效期，约 90 天）并进剪贴板
$expTxt = ''
try {
  $p = $token.Split('.')[1].Replace('-','+').Replace('_','/')
  switch ($p.Length % 4) { 2 { $p += '==' } 3 { $p += '=' } }
  $j = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($p)) | ConvertFrom-Json
  if ($j.exp) { $expTxt = [DateTimeOffset]::FromUnixTimeSeconds([long]$j.exp).ToLocalTime().ToString('yyyy-MM-dd HH:mm') }
} catch {}
$line = if ($Nickname) { "$token-----$Nickname" } else { $token }
Write-Host ''
Write-Host '[4/4] 完成。把下面整行粘进面板 FreePI → 添加凭据（每行一枚）：'
Write-Host '------------------------------------------------------------'
Write-Host $line
Write-Host '------------------------------------------------------------'
if (-not $NoClip) {
  try { Set-Clipboard -Value $line; Write-Host '该行已复制进剪贴板——直接去面板粘贴即可，误关窗口也不会丢。' }
  catch { Write-Host '剪贴板复制失败，请手动选中上面文本复制。' }
}
if ($handle) { Write-Host "GitHub 账号 : $handle" }
if ($expTxt) { Write-Host "JWT 有效期至: $expTxt（无 refresh token；到期重铸后在面板编辑弹窗换明文）" }
} finally {
  # 清空缓冲区里积攒的 Ctrl+C 按键，恢复控制台默认行为（exit/异常路径都会经过这里）
  try { while ([Console]::KeyAvailable) { [void][Console]::ReadKey($true) } } catch {}
  try { [Console]::TreatControlCAsInput = $false } catch {}
}
