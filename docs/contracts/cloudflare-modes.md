# Cloudflare deployment modes

AMOS currently supports explicit DNS-only mode for both aws_managed and aws_vm deployment profiles. Configuration must name a Cloudflare zone ID, its DNS name, the application hostname within that zone, and DNS-only mode.

The mode validator limits alias requests to the selected zone and exact application hostname. It rejects proxied and unspecified proxy modes before an alias adapter receives options. The adapter options always set Proxied to false. Proxy mode remains unavailable until a separate contract covers strict origin TLS, direct-origin bypass, trusted headers, cookies, webhook delivery, and cache bypass.

DNS-only deployment provides DNS records. It does not claim CDN, WAF, or other Cloudflare proxy protection. This contract and its unit tests do not prove a live Cloudflare account, zone ownership, DNS delegation, record changes, or production readiness. An integration task must call the validator before passing the resulting options to the selected upstream DNS alias component.
