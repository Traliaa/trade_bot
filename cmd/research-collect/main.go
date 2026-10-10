//go:build linux || darwin

// research-collect is deliberately separate from the trading bootstrap.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trade_bot/internal/research/collect"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, time.Now())
	stop()
	os.Exit(code)
}

// Inherited descriptors are normally blocking. Set nonblocking BEFORE NewFile
// so Go registers the pipe with its poller and cancellation/deadlines work.
// The command owns these explicit descriptors; no discovery or duplication.
func credentialPipe(fd int) *os.File {
	if fd < 3 {
		return nil
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFIFO {
		return nil
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || unix.SetNonblock(fd, true) != nil {
		return nil
	}
	return os.NewFile(uintptr(fd), "research-credential-pipe")
}

func execute(ctx context.Context, args []string, out io.Writer, now time.Time) int {
	emit := func(value any, code int) int {
		if json.NewEncoder(out).Encode(value) != nil {
			return 1
		}
		return code
	}
	fail := func(reason string, code int) int {
		return emit(map[string]string{"status": "incomplete", "reason": reason}, code)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		_, err := io.WriteString(out, "research-collect: read-only one-shot collector\ninit --store DIR --outside-web-root --protocol ID --start RFC3339\nrun --store DIR --outside-web-root --ref HASH --db-fd N [--okx-fd N]\ninspect --store DIR --ref HASH\nCredentials: explicit inherited pipes only (FD >= 3); never arguments or environment.\nExit: 0 complete, 2 incomplete/inputs pending, 1 error.\n")
		if err != nil {
			return 1
		}
		return 0
	}
	if len(args) == 0 || (args[0] != "init" && args[0] != "run" && args[0] != "inspect") {
		return fail("invalid_arguments", 1)
	}
	flags := flag.NewFlagSet("research-collect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("store", "", "")
	ref := flags.String("ref", "", "")
	outside := flags.Bool("outside-web-root", false, "")
	protocol := flags.String("protocol", "", "")
	start := flags.String("start", "", "")
	dbFD := flags.Int("db-fd", -1, "")
	okxFD := flags.Int("okx-fd", -1, "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return fail("invalid_arguments", 1)
	}
	allowed := map[string]bool{"store": true, "ref": true}
	if args[0] == "init" {
		allowed = map[string]bool{"store": true, "outside-web-root": true, "protocol": true, "start": true}
	}
	if args[0] == "run" {
		allowed["outside-web-root"] = true
		allowed["db-fd"] = true
		allowed["okx-fd"] = true
	}
	bad := false
	flags.Visit(func(f *flag.Flag) {
		if !allowed[f.Name] {
			bad = true
		}
	})
	if bad || *dir == "" {
		return fail("invalid_arguments", 1)
	}
	if args[0] != "inspect" && !*outside {
		return fail("private_volume_acknowledgment_required", 2)
	}
	if args[0] == "run" && (*dbFD < 3 || (*okxFD != -1 && *okxFD < 3) || *dbFD == *okxFD) {
		return fail("credential_pipes_required", 2)
	}
	store, err := collect.OpenStore(*dir, 1<<30)
	if err != nil {
		return fail("private_store_unavailable", 1)
	}
	defer store.Close()
	switch args[0] {
	case "init":
		at, err := time.Parse(time.RFC3339, *start)
		if err != nil {
			return fail("invalid_protocol", 1)
		}
		p := collect.Protocol{Format: "prospective-exit-protocol-v1", ID: *protocol, Start: at, End: at.Add(28 * 24 * time.Hour), HorizonHours: 72, MaxPages: 200, MaxBytes: 1 << 30}
		r, err := collect.RegisterProtocol(store, p, now)
		if err != nil {
			return fail("invalid_protocol", 1)
		}
		return emit(map[string]string{"protocol_ref": r}, 0)
	case "inspect":
		if p, err := collect.LoadProtocol(store, *ref); err == nil {
			return emit(p, 0)
		}
		b, err := collect.ReadBundle(store, *ref)
		if err != nil {
			return fail("invalid_bundle_or_protocol", 1)
		}
		return emit(b, 0)
	}
	p, err := collect.LoadProtocol(store, *ref)
	if err != nil {
		return fail("invalid_protocol", 1)
	}
	now = now.UTC().Truncate(time.Millisecond)
	if !now.After(p.Start) {
		return fail("protocol_not_started", 2)
	}
	dbFile := credentialPipe(*dbFD)
	if dbFile == nil {
		return fail("invalid_db_pipe", 1)
	}
	defer dbFile.Close()
	dbCreds, err := collect.ReadDBCredentials(ctx, dbFile)
	if err != nil {
		return fail("invalid_db_credentials", 1)
	}
	var private *collect.PrivateClient
	if *okxFD >= 3 {
		file := credentialPipe(*okxFD)
		if file == nil {
			return fail("invalid_okx_pipe", 1)
		}
		defer file.Close()
		creds, err := collect.ReadPrivateCredentials(ctx, file)
		if err != nil {
			return fail("invalid_okx_credentials", 1)
		}
		private, err = collect.NewPrivateClient(store, creds)
		if err != nil {
			return fail("invalid_okx_credentials", 1)
		}
	}
	pool, err := dbCreds.OpenPool(ctx)
	if err != nil {
		return fail("db_unavailable", 1)
	}
	defer pool.Close()
	until := p.End
	if now.Before(until) {
		until = now
	}
	batch, err := collect.ReadStudyEntries(ctx, pool, collect.EntryWindow{From: p.Start, To: until, Protocol: p.ID, Limit: 1000})
	if err != nil {
		return fail("db_snapshot_unavailable", 1)
	}
	report, err := collect.RunPass(ctx, store, *ref, batch, collect.NewArchiveClient(store), private, now)
	if err != nil {
		return fail("collection_pass_failed", 1)
	}
	code := 2
	if report.Status == "complete" {
		code = 0
	}
	return emit(report, code)
}
