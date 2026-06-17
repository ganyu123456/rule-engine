package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/kubeedge/rule-engine/config"
	"github.com/kubeedge/rule-engine/engine"
)

func main() {
	klog.InitFlags(nil)
	defer klog.Flush()

	configFile := flag.String("config-file", "/etc/rule-engine/config.yaml", "path to rule engine config file")
	flag.Parse()

	cfg, err := config.Load(*configFile)
	if err != nil {
		klog.Fatalf("load config: %v", err)
	}
	klog.Infof("config loaded from %s: source=%s rules=%d",
		*configFile, cfg.Source.Broker, len(cfg.Rules))

	eng, err := engine.New(cfg)
	if err != nil {
		klog.Fatalf("build engine: %v", err)
	}

	// Start the HTTP management API in the background.
	go startHTTPServer(cfg.HTTP.Port, eng)

	// Block until SIGINT / SIGTERM.
	done := make(chan struct{})
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		klog.Infoln("received shutdown signal")
		close(done)
	}()

	if err := eng.Run(done); err != nil {
		klog.Fatalf("engine error: %v", err)
	}
	klog.Infoln("rule engine exited cleanly")
}

// startHTTPServer runs a lightweight HTTP API for health checks and rule inspection.
func startHTTPServer(port int, eng *engine.Engine) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/api/rules", func(w http.ResponseWriter, _ *http.Request) {
		data, err := eng.MarshalStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/api/rules/stats", func(w http.ResponseWriter, _ *http.Request) {
		statuses := eng.Status()
		var total, dropped, errors int64
		for _, s := range statuses {
			total += s.Processed
			dropped += s.Dropped
			errors += s.Errors
		}
		summary := map[string]interface{}{
			"total_rules":     len(statuses),
			"total_processed": total,
			"total_dropped":   dropped,
			"total_errors":    errors,
		}
		data, _ := json.Marshal(summary)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})

	addr := fmt.Sprintf(":%d", port)
	klog.Infof("HTTP management API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		klog.Errorf("HTTP server error: %v", err)
	}
}
