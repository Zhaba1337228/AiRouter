@echo off
REM ── Wipe stale registry env vars from old router/claudehub ──────────────────
reg delete "HKCU\Environment" /v ANTHROPIC_BASE_URL   /f >nul 2>&1
reg delete "HKCU\Environment" /v ANTHROPIC_API_KEY    /f >nul 2>&1
reg delete "HKCU\Environment" /v ANTHROPIC_AUTH_TOKEN /f >nul 2>&1

REM ── Clear session vars ───────────────────────────────────────────────────────
set ANTHROPIC_BASE_URL=
set ANTHROPIC_API_KEY=
set ANTHROPIC_AUTH_TOKEN=

REM ── Point to local AiRouter ──────────────────────────────────────────────────
set "ANTHROPIC_BASE_URL=http://localhost:8200"
set "ANTHROPIC_API_KEY=ar-720627f359fef0bafed28cefd1d20f3bad12cdd919966163"

echo AiRouter: %ANTHROPIC_BASE_URL%
echo.
claude
