package main

import (
	"crypto/tls"
	"fmt"
	"github.com/miekg/dns"
	"github.com/urfave/cli"
	"log"
	"net"
	"os"
	"time"
)

type Config struct {
	UpStreamResolverIp   string
	UpStreamResolverPort string
	UpStreamResolverHost string // 新增：用于 TLS 证书验证的主机名
	TCPPort              string
	UDPPort              string // 修复拼写错误
	UpstreamTimeout      time.Duration
}

var (
	App = cli.NewApp()
)

func main() {
	conf := Config{
		UpStreamResolverIp:   "185.49.141.38",
		UpStreamResolverPort: "853",
		UpStreamResolverHost: "getdnsapi.net", // 设置正确的主机名用于证书验证
		TCPPort:              ":53",
		UDPPort:              ":53", // 修复拼写错误
		UpstreamTimeout:      time.Millisecond * 3000,
	}

	dns.HandleFunc(".", DNSHandler(conf))

	App.Name = "DNS-over-TLS Proxy"
	App.UsageText = "ex: go run main.go udp"
	App.Commands = []cli.Command{
		{
			Name:  "udp",
			Usage: "run the UDP/53 server",
			Action: func(c *cli.Context) {
				udp := dns.Server{Addr: conf.UDPPort, Net: "udp"}
				StartServer(&udp)
			},
		},
		{
			Name:  "tcp",
			Usage: "run the TCP/53 server",
			Action: func(c *cli.Context) {
				tcp := dns.Server{Addr: conf.TCPPort, Net: "tcp"}
				StartServer(&tcp)
			},
		},
	}

	err := App.Run(os.Args)
	if err != nil {
		panic(err.Error())
	}
}

func StartServer(s *dns.Server) {
	log.Printf("DNS server is running on port %v/%v", s.Net, s.Addr)
	n := ""
	if s.Net == "tcp" {
		n = "+tcp"
	}
	log.Printf("try in cli : dig +short %v google.com @localhost", n)
	err := s.ListenAndServe()
	if err != nil {
		panic(err.Error())
	}
}

// DNS 处理函数
func DNSHandler(conf Config) func(dns.ResponseWriter, *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		// 创建 TLS 配置，使用正确的 ServerName
		tlsConfig := &tls.Config{
			ServerName: conf.UpStreamResolverHost, // 使用域名进行证书验证
		}

		// 建立 TLS 连接
		addr := net.JoinHostPort(conf.UpStreamResolverIp, conf.UpStreamResolverPort)
		conn, err := tls.DialWithDialer(
			&net.Dialer{Timeout: conf.UpstreamTimeout},
			"tcp",
			addr,
			tlsConfig,
		)
		if err != nil {
			log.Printf("Failed to connect to upstream DNS server: %v", err)
			dns.HandleFailed(w, r)
			return
		}
		defer conn.Close()

		// 创建 DNS 客户端
		client := &dns.Client{
			Net:     "tcp-tls",
			Timeout: conf.UpstreamTimeout,
		}

		// 通过 TLS 连接查询上游 DNS
		response, _, err := client.ExchangeWithConn(r, &dns.Conn{Conn: conn})
		if err != nil {
			log.Printf("Failed to query upstream DNS: %v", err)
			dns.HandleFailed(w, r)
			return
		}

		// 返回响应
		err = w.WriteMsg(response)
		if err != nil {
			log.Printf("Failed to write response: %v", err)
		}
	}
}

// 替代方案：使用 dns.Client 的内置 TLS 支持
func DNSHandlerAlternative(conf Config) func(dns.ResponseWriter, *dns.Msg) {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		// 使用内置的 DNS over TLS 客户端
		client := &dns.Client{
			Net:     "tcp-tls",
			Timeout: conf.UpstreamTimeout,
			// 设置 TLS 配置
			TLSConfig: &tls.Config{
				ServerName: conf.UpStreamResolverHost,
			},
		}

		// 构建上游服务器地址，使用域名而不是 IP
		upstreamAddr := fmt.Sprintf("%s:%s", conf.UpStreamResolverHost, conf.UpStreamResolverPort)
		
		// 但是实际连接时需要解析到 IP，这里我们手动指定
		// 或者可以修改为直接使用 IP + ServerName 的方式
		upstreamAddr = fmt.Sprintf("%s:%s", conf.UpStreamResolverIp, conf.UpStreamResolverPort)

		response, _, err := client.Exchange(r, upstreamAddr)
		if err != nil {
			log.Printf("Failed to query upstream DNS: %v", err)
			dns.HandleFailed(w, r)
			return
		}

		err = w.WriteMsg(response)
		if err != nil {
			log.Printf("Failed to write response: %v", err)
		}
	}
}
