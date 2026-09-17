#!/usr/bin/env sh
set -eu

domain="${1:-}"
expected="${2:-}"
if [ -z "$domain" ]; then
  echo "Usage: $0 <domain> [expected-public-ipv4]" >&2
  exit 2
fi
if ! command -v getent >/dev/null 2>&1; then
  echo "ERROR: getent is required (package libc-bin on Debian/Ubuntu)." >&2
  exit 2
fi

ipv4="$(getent ahostsv4 "$domain" 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ')"
ipv6="$(getent ahostsv6 "$domain" 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ')"
if [ -z "$ipv4" ] && [ -z "$ipv6" ]; then
  echo "ERROR: $domain does not exist in public DNS (no A or AAAA result)." >&2
  echo "Create an A record and wait for DNS propagation before starting Caddy." >&2
  exit 1
fi

echo "Domain: $domain"
echo "A:     ${ipv4:-<none>}"
echo "AAAA:  ${ipv6:-<none>}"
if [ -n "$expected" ] && ! printf '%s\n' "$ipv4" | tr ' ' '\n' | grep -Fxq "$expected"; then
  echo "ERROR: expected IPv4 $expected is not present in the A results." >&2
  exit 1
fi
if [ -n "$ipv6" ]; then
  echo "WARNING: AAAA exists. Keep it only if TCP 80/443 work over IPv6." >&2
fi
echo "DNS preflight passed. Also verify TCP ports 80 and 443 from outside the server."
