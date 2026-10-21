package server

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/twistingmercury/mnemonic/internal/api/config"
)

func TestCreateHTTPServerUsesAdminAPIConfiguration(t *testing.T) {
	t.Parallel()

	cfg := &config.MnemonicConfig{
		Server: config.ServerConfig{
			Host:         "127.0.0.1",
			Port:         9090,
			ReadTimeout:  time.Second,
			WriteTimeout: 2 * time.Second,
			IdleTimeout:  3 * time.Second,
		},
	}

	srv := CreateHTTPServer(gin.New(), cfg)

	assert.Equal(t, "127.0.0.1:9090", srv.Addr)
	assert.Equal(t, cfg.Server.ReadTimeout, srv.ReadTimeout)
	assert.Equal(t, cfg.Server.WriteTimeout, srv.WriteTimeout)
	assert.Equal(t, cfg.Server.IdleTimeout, srv.IdleTimeout)
}
