package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	_ "net/http/pprof"

	"github.com/go-log/log"
	gost "github.com/ngvpn/tunnel"
)

var (
	configureFile string
	baseCfg       = &baseConfig{}
	pprofAddr     string
	pprofEnabled  = os.Getenv("PROFILING") != ""
	logEnabled    = os.Getenv("LOG") != ""
)

func init() {
	gost.SetLogger(&gost.LogLogger{})

	var (
		printVersion bool
	)

	flag.Var(&baseCfg.route.ChainNodes, "F", "forward address, can make a forward chain")
	flag.Var(&baseCfg.route.ServeNodes, "L", "listen address, can listen on multiple ports (required)")
	flag.StringVar(&configureFile, "C", "", "configure file")
	flag.BoolVar(&baseCfg.Debug, "D", false, "enable debug log")
	flag.BoolVar(&printVersion, "V", false, "print version")
	if pprofEnabled {
		flag.StringVar(&pprofAddr, "P", ":6060", "profiling HTTP server address")
	}
	flag.Parse()

	if printVersion {
		fmt.Fprintf(os.Stdout, "gost %s (%s %s/%s)\n",
			gost.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		flag.PrintDefaults()
		os.Exit(0)
	}

	if configureFile != "" {
		_, err := parseBaseConfig(configureFile)
		if err != nil {
			log.Log(err)
			os.Exit(1)
		}
	}

	if freeMem := os.Getenv("FREEMEM"); freeMem != "" {
		debug.SetGCPercent(10)
		if seconds, err := strconv.Atoi(freeMem); err == nil {
			ticker := time.NewTicker(time.Second * time.Duration(seconds))
			go func() {
				for range ticker.C {
					debug.FreeOSMemory()
				}
			}()
		}
	}

	if flag.NFlag() == 0 {
		port := os.Getenv("PORT")
		if port == "" {
			port = "3000"
		}
		if !logEnabled {
			gost.SetLogger(&gost.NopLogger{})
		}
		fmt.Fprintf(os.Stdout, "ngvpn-edge %s (%s %s/%s)\n",
			gost.Version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		userpwd := os.Getenv("USER")
		if userpwd == "" {
			userpwd = "ngvpn:edge@"
		}
		wsRelayPath := os.Getenv("WSPATH")
		if wsRelayPath == "" {
			wsRelayPath = "ngvpn-edge-ws"
		}
		mwsRelayPath := os.Getenv("MWSPATH")
		if mwsRelayPath == "" {
			mwsRelayPath = "ngvpn-edge-mws"
		}
		wsSocksPath := os.Getenv("WSSOCKSPATH")
		if wsSocksPath == "" {
			wsSocksPath = "ngvpn-edge-ws-socks"
		}
		mwsSocksPath := os.Getenv("MWSSOCKSPATH")
		if mwsSocksPath == "" {
			mwsSocksPath = "ngvpn-edge-mws-socks"
		}
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("relay+ws://%v:%v?path=/%s&reverseproxy=/%s@http://localhost:2054/ws,/%s@http://localhost:2055/ws,/%s@http://localhost:2056/ws",
			userpwd, port, wsRelayPath, mwsRelayPath, wsSocksPath, mwsSocksPath))
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("relay+mws://%v127.0.0.1:2054", userpwd))
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("ws://%v127.0.0.1:2055", userpwd))
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("mws://%v127.0.0.1:2056", userpwd))
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("h2c://%v127.0.0.1:2057", userpwd))
		baseCfg.route.ServeNodes.Set(fmt.Sprintf("quic://%v127.0.0.1:2058", userpwd))
	}
}

func main() {
	if pprofEnabled {
		go func() {
			log.Log("profiling server on", pprofAddr)
			log.Log(http.ListenAndServe(pprofAddr, nil))
		}()
	}

	// NOTE: as of 2.6, you can use custom cert/key files to initialize the default certificate.
	tlsConfig, err := tlsConfig(defaultCertFile, defaultKeyFile, "")
	if err != nil {
		// generate random self-signed certificate.
		cert, err := gost.GenCertificate()
		if err != nil {
			log.Log(err)
			os.Exit(1)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	} else {
		log.Log("load TLS certificate files OK")
	}

	gost.DefaultTLSConfig = tlsConfig

	if err := start(); err != nil {
		log.Log(err)
		os.Exit(1)
	}

	select {}
}

func start() error {
	gost.Debug = baseCfg.Debug

	var routers []router
	rts, err := baseCfg.route.GenRouters()
	if err != nil {
		return err
	}
	routers = append(routers, rts...)

	for _, route := range baseCfg.Routes {
		rts, err := route.GenRouters()
		if err != nil {
			return err
		}
		routers = append(routers, rts...)
	}

	if len(routers) == 0 {
		return errors.New("invalid config")
	}
	for i := range routers {
		go routers[i].Serve()
	}

	return nil
}
