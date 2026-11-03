#!/bin/bash
# Double-click in Finder. SSH uses this computer's existing login identity.
url=http://127.0.0.1:18765
if /usr/bin/curl --noproxy '*' -fsS --max-time 1 "$url/api/health" >/dev/null 2>&1; then
  /usr/bin/open "$url/"
  exit 0
fi

printf '正在连接数字人，连接后会自动打开网页。\n保持这个窗口打开，按 Control+C 断开。\n'
(
  until /usr/bin/curl --noproxy '*' -fsS --max-time 1 "$url/api/health" >/dev/null 2>&1; do sleep 1; done
  /usr/bin/open "$url/"
) &
opener=$!
trap 'kill "$opener" 2>/dev/null || true' EXIT
trap 'exit 0' INT TERM HUP

/usr/bin/ssh -N -p 5307 -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 \
  -L 127.0.0.1:18765:127.0.0.1:8765 huangmingru@10.201.10.173
result=$?
if [ "$result" -ne 0 ]; then
  printf '\n连接未成功，请查看上面的 SSH 提示。按回车关闭。\n'
  read -r _
fi
exit "$result"
