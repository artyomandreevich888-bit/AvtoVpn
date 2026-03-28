package frontend

import "embed"

//go:embed index.html style.css main.js
var Assets embed.FS
