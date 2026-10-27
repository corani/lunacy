package static

import "embed"

//go:embed index.html style.css app.js
var FS embed.FS
