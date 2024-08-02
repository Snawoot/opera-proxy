opera-proxy
===========

[![opera-proxy](https://snapcraft.io//opera-proxy/badge.svg)](https://snapcraft.io/opera-proxy)

<img align="left" width="100" height="100" src="https://github.com/user-attachments/assets/aac4d29a-32b3-4e00-957c-3c7168228edc">

Standalone Opera VPN client. Younger brother of [hola-proxy](https://github.com/Snawoot/hola-proxy/).

Just run it and it'll start a plain HTTP proxy server forwarding traffic through "Opera VPN" proxies of your choice.
By default the application listens on 127.0.0.1:18080.

## Features

* Cross-platform (Windows/Mac OS/Linux/Android (via shell)/\*BSD)
* Uses TLS for secure communication with upstream proxies
* Zero configuration
* Simple and straightforward

## Installation

#### Binaries

Pre-built binaries are available [here](https://github.com/Snawoot/opera-proxy/releases/latest).

#### Build from source

Alternatively, you may install opera-proxy from source. Run the following within the source directory:

```
make install
```

#### Docker

A docker image is available as well. Here is an example of running opera-proxy as a background service:

```sh
docker run -d \
    --security-opt no-new-privileges \
    -p 127.0.0.1:18080:18080 \
    --restart unless-stopped \
    --name opera-proxy \
    yarmak/opera-proxy
```

#### Snap Store

[![Get it from the Snap Store](https://snapcraft.io/static/images/badges/en/snap-store-black.svg)](https://snapcraft.io/opera-proxy)

```bash
sudo snap install opera-proxy
```

## Usage

List available countries:

```
$ ./opera-proxy -list-countries
country code,country name
EU,Europe
AS,Asia
AM,Americas
```

Run proxy via country of your choice:

```
$ ./opera-proxy -country EU
```

Also it is possible to export proxy addresses and credentials:

```
$ ./opera-proxy -country EU -list-proxies
Proxy login: ABCF206831D0BDC0C8C3AE5283F99EF6726444B3
Proxy password: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb3VudHJ5IjoidWEiLCJpYXQiOjE2MTY4MDkxMTIsImlkIjoic2UwMzE2LTYweGY3aTBxMGhoOWQ1MWF0emd0IiwiaXAiOiI3Ny4xMTEuMjQ3LjE3IiwidnBuX2xvZ2luIjoiSzJYdmJ5R0tUb3JLbkpOaDNtUGlGSTJvSytyVTA5bXMraGt2c2UwRWJBcz1Ac2UwMzE2LmJlc3QudnBuIn0.ZhqqzVyKmc3hZG6VVwWfn4nvVIPuZvaEfOLXfTppyvo
Proxy-Authorization: Basic QUJDRjIwNjgzMUQwQkRDMEM4QzNBRTUyODNGOTlFRjY3MjY0NDRCMzpleUpoYkdjaU9pSklVekkxTmlJc0luUjVjQ0k2SWtwWFZDSjkuZXlKamIzVnVkSEo1SWpvaWRXRWlMQ0pwWVhRaU9qRTJNVFk0TURreE1USXNJbWxrSWpvaWMyVXdNekUyTFRZd2VHWTNhVEJ4TUdob09XUTFNV0YwZW1kMElpd2lhWEFpT2lJM055NHhNVEV1TWpRM0xqRTNJaXdpZG5CdVgyeHZaMmx1SWpvaVN6SllkbUo1UjB0VWIzSkxia3BPYUROdFVHbEdTVEp2U3l0eVZUQTViWE1yYUd0MmMyVXdSV0pCY3oxQWMyVXdNekUyTG1KbGMzUXVkbkJ1SW4wLlpocXF6VnlLbWMzaFpHNlZWd1dmbjRudlZJUHVadmFFZk9MWGZUcHB5dm8=

host,ip_address,port
eu0.sec-tunnel.com,77.111.244.26,443
eu1.sec-tunnel.com,77.111.244.67,443
eu2.sec-tunnel.com,77.111.247.51,443
eu3.sec-tunnel.com,77.111.244.22,443
```

## List of arguments

| Argument | Type | Description |
| -------- | ---- | ----------- |
| api-address | String | override IP address of api.sec-tunnel.com |
| api-login | String | SurfEasy API login (default "se0316") |
| api-password | String | SurfEasy API password (default "SILrMEPBmJuhomxWkfm3JalqHX2Eheg1YhlEZiMh8II") |
| bind-address | String | HTTP proxy listen address (default "127.0.0.1:18080") |
| bootstrap-dns | String | Comma-separated list of DNS/DoH/DoT/DoQ resolvers for initial discovery of SurfEasy API address. See https://github.com/ameshkov/dnslookup/ for upstream DNS URL format. Examples: `https://1.1.1.1/dns-query`, `quic://dns.adguard.com`  (default `https://1.1.1.3/dns-query,https://8.8.8.8/dns-query,https://dns.google/dns-query,https://security.cloudflare-dns.com/dns-query,https://wikimedia-dns.org/dns-query,https://dns.adguard-dns.com/dns-query,https://dns.quad9.net/dns-query,https://doh.cleanbrowsing.org/doh/adult-filter/`) |
| cafile | String | use custom CA certificate bundle file |
| certchain-workaround | Boolean | add bundled cross-signed intermediate cert to certchain to make it check out on old systems (default true) |
| country | String | desired proxy location (default "EU") |
| list-countries | - | list available countries and exit |
| list-proxies | - | output proxy list and exit |
| proxy | String | sets base proxy to use for all dial-outs. Format: `<http\|https\|socks5\|socks5h>://[login:password@]host[:port]` Examples: `http://user:password@192.168.1.1:3128`, `socks5://10.0.0.1:1080` |
| refresh | Duration | login refresh interval (default 4h0m0s) |
| refresh-retry | Duration | login refresh retry interval (default 5s) |
| timeout | Duration | timeout for network operations (default 10s) |
| verbosity | Number | logging verbosity (10 - debug, 20 - info, 30 - warning, 40 - error, 50 - critical) (default 20) |
| version | - | show program version and exit |

## See also

* [Project wiki](https://github.com/Snawoot/opera-proxy/wiki)
* [Community in Telegram](https://t.me/alternative_proxy)
