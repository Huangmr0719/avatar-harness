@echo off
title Avatar Harness
echo Connecting to Avatar Harness. The browser will open when ready.
echo Keep this window open. Press Ctrl+C to disconnect.
start "" /b powershell.exe -NoProfile -Command "$ProgressPreference='SilentlyContinue'; for($i=0; $i -lt 120; $i++){try{$h=Invoke-RestMethod 'http://127.0.0.1:18765/api/health' -TimeoutSec 1; if($h.status -eq 'ok' -and $h.runtime -eq 'go'){Start-Process 'http://127.0.0.1:18765/'; exit}}catch{}; Start-Sleep -Seconds 1}"
ssh -N -p 5307 -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -L 127.0.0.1:18765:127.0.0.1:8765 huangmingru@10.201.10.173
if errorlevel 1 (
  echo Connection failed. Check the SSH message above.
  pause
)
