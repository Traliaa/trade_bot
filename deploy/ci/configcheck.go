// Image smoke test: no application initialization, network or production inputs.
package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Geteuid() != 65532 {
		fmt.Fprintln(os.Stderr, "EXPECTED_NONROOT_UID_65532")
		os.Exit(1)
	}
	b, err := os.ReadFile("/app/configs/values_ci.yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, "CONFIG_READ_FAILED:", err)
		os.Exit(1)
	}
	if len(b) == 0 {
		fmt.Fprintln(os.Stderr, "CONFIG_EMPTY")
		os.Exit(1)
	}
	fmt.Println("CONFIG_READABLE_AS_NONROOT")
}
