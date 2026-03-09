package main

import (
	cfg "github.com/conductorone/baton-zoom/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("zoom", cfg.Config)
}
