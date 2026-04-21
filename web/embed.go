package web

import "embed"

// StaticFS embeds the static web assets.
//
//go:embed static/*
var StaticFS embed.FS
