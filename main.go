package main

// main is a tiny entrypoint that delegates initialization and server startup
// to Run() implemented in server.go. Keeping this file minimal avoids duplicate
// declarations and keeps responsibilities focused.
func main() {
	Run()
}
