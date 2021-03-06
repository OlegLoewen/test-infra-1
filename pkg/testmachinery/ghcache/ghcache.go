// Copyright 2019 Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ghcache

import (
	"github.com/go-logr/logr"
	"github.com/gregjones/httpcache"
	"github.com/gregjones/httpcache/diskcache"
	"github.com/peterbourgon/diskv"
	"net/http"
	"os"
	"path"

	"github.com/pkg/errors"
	flag "github.com/spf13/pflag"
)

// Cache adds github caching to a http client.
// It returns a mem cache by default and a disk cache if a directory is defined
func Cache(log logr.Logger, cfg *Config, delegate http.RoundTripper) (http.RoundTripper, error) {
	if cfg == nil && config == nil {
		return nil, errors.New("no configuration is provided for the github cache")
	}
	if cfg == nil {
		cfg = config
	}

	githubCache, err := getCache(cfg)
	if err != nil {
		return nil, err
	}

	cachedTransport := httpcache.NewTransport(githubCache)
	cachedTransport.Transport = &cache{
		delegate:      delegate,
		maxAgeSeconds: cfg.MaxAgeSeconds,
	}

	return &rateLimitLogger{
		log:      log,
		delegate: cachedTransport,
	}, nil

}

func getCache(cfg *Config) (httpcache.Cache, error) {
	if cfg.CacheDir == "" {
		return httpcache.NewMemoryCache(), nil
	}

	if err := os.MkdirAll(cfg.CacheDir, os.ModePerm); err != nil {
		return nil, err
	}
	if cfg.CacheDiskSizeGB == 0 {
		return nil, errors.New("disk cache size ha to be grater than 0")
	}

	return diskcache.NewWithDiskv(
		diskv.New(diskv.Options{
			BasePath:     path.Join(cfg.CacheDir, "data"),
			TempDir:      path.Join(cfg.CacheDir, "temp"),
			CacheSizeMax: uint64(cfg.CacheDiskSizeGB) * uint64(1000000000), // GB to B
		})), nil
}

// Config is the github cache configuration
type Config struct {
	CacheDir        string
	CacheDiskSizeGB int
	MaxAgeSeconds   int
}

var config *Config

// DeepCopy copies the configuration object
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return &Config{}
	}
	cfg := *c
	return &cfg
}

func InitFlags(flagset *flag.FlagSet) *Config {
	if flagset == nil {
		flagset = flag.CommandLine
	}
	config = &Config{}
	flagset.StringVar(&config.CacheDir, "github-cache-dir", "",
		"Path directory that should be used to cache github requests")
	flagset.IntVar(&config.CacheDiskSizeGB, "github-cache-size", 1,
		"Size of the github cache in GB")
	flagset.IntVar(&config.MaxAgeSeconds, "github-cache-max-age", 600,
		"Maximum age of a failed github response in seconds")
	return config
}
