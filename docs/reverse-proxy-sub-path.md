# GOtome under a sub-path

Behind a reverse proxy, GOtome can answer at `https://example.org/gotome/`
instead of at a host name of its own. Set, in `.env`:

```sh
GOTOME_BASE_PATH=/gotome/
```

and `docker compose up -d`.

## What the proxy must do

GOtome serves itself under the prefix: it expects `/gotome/` in the path
of every request, and it answers a path outside it with 404. So the proxy
**passes the prefix on and does not strip it**. (Stripping would be the
other way to do this, but GOtome then could not tell the addresses it hands
out, such as the single sign-on callback or the Kobo reader's endpoint, from
its own.) Everything else is as for a host name of its own
([getting-started.md](getting-started.md#a-reverse-proxy-and-https)): the
`Host` the browser used, `X-Forwarded-Proto`, `X-Forwarded-For`, room for
uploads, and `GOTOME_TRUSTED_PROXIES`.

`/gotome` without the slash is redirected to `/gotome/`. The probes
`/healthz` and `/readyz` also stay at the root of the container's own port,
which is what the container's health check asks.

## What follows the prefix

- the web app and its API (`/gotome/api/v1/...`);
- the OPDS catalogue, its links and its search, and the Kobo reader's
  `api_endpoint` (`https://example.org/gotome/api/v1/kobo/<token>`) with the
  resource map it answers;
- the KOReader plugin's download, which is told
  `https://example.org/gotome` as its server, and the progress sync address
  you enter in KOReader (`https://example.org/gotome/api/v1/kosync`);
- the session cookie's and the single sign-on cookie's `Path`;
- the single sign-on redirect address: register
  `https://example.org/gotome/api/v1/auth/oidc/callback` with the identity
  provider (the settings page shows it);
- redirects the server makes, such as the end of a sign-in.

The Content Security Policy needs no change: it allows only the page's own
origin, whatever its path. The build of the web app holds only relative
addresses, so the same image serves any path.

Change `GOTOME_BASE_PATH` and the addresses already given out (an
e-reader's catalogue, a Kobo's endpoint, KOReader's server) have to be
entered again.

## Caddy

`handle` keeps the path as it is; `handle_path` would strip it, so do not
use that here.

```caddyfile
example.org {
    handle /gotome/* {
        reverse_proxy 127.0.0.1:8080
    }
    redir /gotome /gotome/
}
```

Caddy sends `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` itself, and
sets no limit on the size of a request body.

## nginx

`proxy_pass` without a path after the address passes the request on as it
came, prefix included.

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name example.org;

    client_max_body_size 0;

    location /gotome/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_request_buffering off;
    }
}
```

Do not write `proxy_pass http://127.0.0.1:8080/;` (with a trailing slash):
that replaces the matched prefix and strips it.

## Traefik

With labels on the app service in Docker Compose. The router matches the
prefix, and no `stripprefix` middleware is added.

```yaml
services:
  app:
    labels:
      - traefik.enable=true
      - traefik.http.routers.gotome.rule=Host(`example.org`) && PathPrefix(`/gotome`)
      - traefik.http.routers.gotome.entrypoints=websecure
      - traefik.http.routers.gotome.tls.certresolver=letsencrypt
      - traefik.http.services.gotome.loadbalancer.server.port=8080
```

Traefik sends the forwarding headers; add its network's range to
`GOTOME_TRUSTED_PROXIES`.

## Trying it

```sh
make up GOTOME_BASE_PATH=/gotome/
make test-e2e-subpath GOTOME_BASE_PATH=/gotome/
```

starts the development stack under the prefix and runs the browser spec
for it.
