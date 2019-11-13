# go-rproxy

A simple PoC reverse proxy designed to make basic docker deployments easier.

This project was born out of the frustrations with other reverse proxy setups.
Many of them are either extremely complicated or don't handle docker containers
automatically or are missing other features.

It is designed as an opinionated reverse proxy and will not work for all use
cases.

## Features

Upcoming features:

- Use docker labels to determine where to proxy
- Automatically acquire TLS certs from letsencrypt
- Handle http (and redirect all http to https)
- Easily run in docker

Wishlisted features:

- Handle tcp (with SSL termination)

## Label examples

- `rproxy.frontend`
    - http://coded.io/blog
      match all https requests going to https://coded.io/blog (and all sub-paths).
      This will additionally handle redirecting from http to https.
    - tcp://:80/SSH-2.0
      match all tcp connections on port 80 beginning with a magic string of SSH-2.0.
      Note that this can be used in conjunction with http handlers.

- `rproxy.backend`
    - http://:8000
      send all requests to the docker container this is attached to on port 8000.
    - http://coded.io/blog
      send all requests to http://coded.io/blog

Labels can be used in either the singular form (`rproxy.backend`) or the plural
form (`rproxy.frontend.name`). Using the plural form will allow you to specify
multiple proxys for a single container. Also note that any named frontend will
match up with a backend with the same name. If a non-singular frontend or
backend is missing its match, it will be disabled.
