# TD-13. Cloudflare in front of the demo

- **Area:** Operations
- **Decision:** TD-13. Cloudflare in front of the demo
- **Default:** Free plan
- **Outcome:** Cloudflare's free plan in front of nginx for the public demo. Absorbs attacks, hides the origin IP, and caches static files.
- **Status:** Decided

## Notes

The origin firewall accepts only Cloudflare's IP ranges, and nginx reads the real client IP from Cloudflare's header so rate limits still work.
