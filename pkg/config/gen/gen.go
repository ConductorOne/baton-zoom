package main

import (
	"github.com/conductorone/baton-sdk/pkg/config"
	cfg "github.com/conductorone/baton-zoom/pkg/config"
)

func main() {
	config.Generate("zoom", cfg.Config)
}
