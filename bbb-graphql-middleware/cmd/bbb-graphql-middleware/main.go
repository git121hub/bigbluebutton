package main

import (
	"bbb-graphql-middleware/internal/common"
	"bbb-graphql-middleware/internal/msgpatch"
	"bbb-graphql-middleware/internal/websrv"
	"context"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	// Configure logger
	if logLevelFromEnvVar, err := log.ParseLevel(os.Getenv("BBB_GRAPHQL_MIDDLEWARE_LOG_LEVEL")); err == nil {
		log.SetLevel(logLevelFromEnvVar)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	log.SetFormatter(&log.JSONFormatter{})
	log := log.WithField("_routine", "main")

	if activitiesOverviewEnabled := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_ACTIVITIES_OVERVIEW_ENABLED"); activitiesOverviewEnabled == "true" {
		go common.ActivitiesOverviewLogRoutine()
		//go common.JsonPatchBenchmarkingLogRoutine()
	}

	common.InitUniqueID()
	log = log.WithField("graphql-middleware-uid", common.GetUniqueID())

	log.Infof("Logger level=%v", log.Logger.Level)

	//Clear cache from last exec
	msgpatch.ClearAllCaches()

	// Listen msgs from akka (for example to invalidate connection)
	go websrv.StartRedisListener()

	if jsonPatchDisabled := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_JSON_PATCH_DISABLED"); jsonPatchDisabled != "" {
		log.Infof("Json Patch Disabled!")
	}

	//if rawDataCacheStorageMode := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_RAW_DATA_CACHE_STORAGE_MODE"); rawDataCacheStorageMode == "file" {
	//	msgpatch.RawDataCacheStorageMode = "file"
	//} else {
	//	msgpatch.RawDataCacheStorageMode = "memory"
	//}
	//Force memory cache for now
	msgpatch.RawDataCacheStorageMode = "memory"

	log.Infof("Raw Data Cache Storage Mode: %s", msgpatch.RawDataCacheStorageMode)

	// Websocket listener

	//Define IP to listen
	listenIp := "127.0.0.1"
	if envListenIp := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_LISTEN_IP"); envListenIp != "" {
		listenIp = envListenIp
	}

	// Define port to listen on
	listenPort := 8378
	if envListenPort := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_LISTEN_PORT"); envListenPort != "" {
		if envListenPortAsInt, err := strconv.Atoi(envListenPort); err == nil {
			listenPort = envListenPortAsInt
		}
	}

	//Define new Connections Rate Limit
	maxConnPerSecond := 10
	if envMaxConnPerSecond := os.Getenv("BBB_GRAPHQL_MIDDLEWARE_MAX_CONN_PER_SECOND"); envMaxConnPerSecond != "" {
		if envMaxConnPerSecondAsInt, err := strconv.Atoi(envMaxConnPerSecond); err == nil {
			maxConnPerSecond = envMaxConnPerSecondAsInt
		}
	}
	rateLimiter := common.NewCustomRateLimiter(maxConnPerSecond)

	http.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()

		common.ActivitiesOverviewStarted("__WebsocketConnection")
		defer common.ActivitiesOverviewCompleted("__WebsocketConnection")

		common.HttpConnectionGauge.Inc()
		common.HttpConnectionCounter.Inc()
		defer common.HttpConnectionGauge.Dec()

		if err := rateLimiter.Wait(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				http.Error(w, "Request cancelled or rate limit exceeded", http.StatusTooManyRequests)
			}

			return
		}

		websrv.ConnectionHandler(w, r)
	})

	// Add Prometheus metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	log.Infof("listening on %v:%v", listenIp, listenPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%v:%v", listenIp, listenPort), nil))

}
