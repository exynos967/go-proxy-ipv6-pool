# Go Proxy IPV6 Pool

Random ipv6 egress proxy server (support http/socks5) 

The Go language implementation of [zu1k/http-proxy-ipv6-pool](https://github.com/zu1k/http-proxy-ipv6-pool)

## Usage

```bash
    go run . --port <port> --cidr < your ipv6 cidr >  # e.g. 2001:399:8205:ae00::/64
```

```bash
    go run . --port <port> --cidr <your ipv6 cidr> --dial-parallel <count>
```

The program will automatically load `.env` from the current working directory.
Default values come from `.env`, and command-line flags still take precedence.

```bash
    cp .env.example .env
    go run .
```

Enable authentication for both the HTTP proxy and the SOCKS5 proxy:

```bash
    go run . --port <port> --cidr <your ipv6 cidr> --username <user> --password <pass>
```

## Network prerequisites

This project assumes the whole IPv6 subnet is actually usable on the machine.

At minimum, verify the following before blaming the proxy itself:

```bash
# allow binding random IPv6 addresses from the routed subnet
sysctl net.ipv6.ip_nonlocal_bind=1

# make the whole prefix local on the outbound interface
ip route add local <your-ipv6-cidr> dev <your-interface>
```

If your provider requires NDP proxying for the routed `/64`, configure `ndppd`
or an equivalent NDP proxy first. Otherwise some randomly selected IPv6
addresses may not receive return traffic correctly.

You can validate the subnet outside the proxy with a direct bind test:

```bash
curl --interface <one-ipv6-from-your-cidr> https://ifconfig.co/ip
```

If the direct bind test is unstable, the problem is in host/provider IPv6 route
or NDP setup rather than in the proxy process.

### Use as a proxy server

```bash
    curl -x http://xxx:52122 http://6.ipw.cn/ # 2001:399:8205:ae00:456a:ab12 (random ipv6 address)
```

```bash
    curl -x socks5://xxx:52123 http://6.ipw.cn/ # 2001:399:8205:ae00:456a:ab12 (random ipv6 address)
```

With authentication enabled:

```bash
    curl -U <user>:<pass> -x http://xxx:52122 http://6.ipw.cn/
```

```bash
    curl --proxy-user <user>:<pass> --socks5-hostname xxx:52123 http://6.ipw.cn/
```

## License

MIT License (see [LICENSE](LICENSE))
