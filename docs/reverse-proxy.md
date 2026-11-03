# Reverse proxy

Pocket Sphere speaks plain HTTP on one port and serves the UI, `/api/v1/*`, the video streams and the headset feeds (`/vr/<key>/…`) from the same binary. Set `BASE_URL` to the public URL and `SECURE_COOKIES=true` behind HTTPS.

What matters for a proxy:

- **Big responses, streamed.** Videos are sent with ranges (seeking) and converted streams are written as they are made. Don't buffer responses (`flush_interval -1` in Caddy, `proxy_buffering off` in nginx) and don't set short read timeouts.
- **Uploads.** Covers and photos are small, but *Bring Stash over* uploads Stash's database (can be hundreds of MB): allow big request bodies.
- **WebXR** (watching in a headset's browser) needs HTTPS.

## Caddy

```
sphere.example.com {
	reverse_proxy pocket-sphere:8080 {
		flush_interval -1
	}
}
```

## nginx

```
location / {
    proxy_pass http://pocket-sphere:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_read_timeout 3600s;
    client_max_body_size 4g;
}
```

## Headset players

DeoVR and HereSphere cannot sign in; each person makes their own address under Settings → Headset (`https://sphere.example.com/vr/<secret>/deovr` and `/heresphere`). Anyone with the address sees that person's library, so keep it on your own network (or behind your VPN) and make a new one if it leaks.
