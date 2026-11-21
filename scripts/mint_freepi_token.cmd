@echo off
rem Mint FreePI JWT (GitHub device flow) via mint_freepi_token.ps1; args pass through.
rem Window stays open (cmd /k) - close it manually (X) or type exit.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0mint_freepi_token.ps1" %*
cmd /k
