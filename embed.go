package main

import "embed"

//go:embed web/templates/* web/static/js/* web/static/css/*
var WebFS embed.FS
