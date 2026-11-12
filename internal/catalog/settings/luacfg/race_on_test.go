//go:build race

package luacfg

// raceOn is true in -race builds, which run gopher-lua several times slower.
const raceOn = true
