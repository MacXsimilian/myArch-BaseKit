package main

// BuildID is the application version shown by the CLI and saved in run reports.
// Release builds override it with -ldflags "-X main.BuildID=vMAJOR.MINOR.PATCH".
var BuildID = "v0.1.0"
