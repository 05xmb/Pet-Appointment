package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/05xmb/Pet-Appointment/internal/auth"
	"github.com/05xmb/Pet-Appointment/internal/booking"
	"github.com/05xmb/Pet-Appointment/internal/cancellation"
	"github.com/05xmb/Pet-Appointment/internal/fulfillment"
	"github.com/05xmb/Pet-Appointment/internal/httpapi"
	"github.com/05xmb/Pet-Appointment/internal/payment"
	"github.com/05xmb/Pet-Appointment/internal/reporting"
	mysqlstore "github.com/05xmb/Pet-Appointment/internal/store/mysql"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/term"
)

type config struct {
	mode, username, dsn, cert, key, addr, paymentSecret string
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	if len(args) == 0 || getenv("PET_MYSQL_DSN") == "" {
		return config{}, errors.New("usage: PET_MYSQL_DSN is required; use init, set-password <username>, or serve")
	}
	cfg := config{mode: args[0], dsn: getenv("PET_MYSQL_DSN")}
	switch cfg.mode {
	case "init":
		if len(args) != 1 {
			return config{}, errors.New("usage: init")
		}
	case "set-password":
		if len(args) != 2 || args[1] == "" {
			return config{}, errors.New("usage: set-password <username>")
		}
		cfg.username = args[1]
	case "serve":
		if len(args) != 1 {
			return config{}, errors.New("usage: serve")
		}
		cfg.cert, cfg.key = getenv("PET_TLS_CERT"), getenv("PET_TLS_KEY")
		cfg.paymentSecret = getenv("PET_PAYMENT_HMAC_SECRET")
		if cfg.cert == "" || cfg.key == "" || cfg.paymentSecret == "" {
			return config{}, errors.New("PET_TLS_CERT, PET_TLS_KEY and PET_PAYMENT_HMAC_SECRET are required for serve")
		}
		cfg.addr = getenv("PET_LISTEN_ADDR")
		if cfg.addr == "" {
			cfg.addr = "127.0.0.1:8443"
		}
	default:
		return config{}, errors.New("usage: init, set-password <username>, or serve")
	}
	return cfg, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return runWithPrompt(args, readConfirmedPassword)
}

func runWithPrompt(args []string, prompt func(string) ([]byte, error)) error {
	cfg, err := parseConfig(args, os.Getenv)
	if err != nil {
		return err
	}
	driverConfig, err := mysql.ParseDSN(cfg.dsn)
	if err != nil || driverConfig.DBName == "" {
		return errors.New("PET_MYSQL_DSN must name an existing database")
	}
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		return errors.New("cannot open MySQL connection")
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("cannot connect to MySQL")
	}
	store := mysqlstore.New(db)
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	switch cfg.mode {
	case "init":
		if err := store.SeedTeachingData(ctx, time.Now()); err != nil {
			return fmt.Errorf("teaching data initialization failed: %w", err)
		}
		names, err := store.UnconfiguredAccounts(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Initialized teaching data. Accounts needing passwords: %v\n", names)
		return nil
	case "set-password":
		password, err := prompt(cfg.username)
		if err != nil {
			return err
		}
		defer clear(password)
		if err := store.ProvisionPassword(ctx, cfg.username, password); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Account password updated.")
		return nil
	case "serve":
		names, err := store.UnconfiguredAccounts(ctx)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			return fmt.Errorf("set passwords before serving: %v", names)
		}
		handler := httpapi.NewHandler(auth.NewService(store, time.Now), store,
			httpapi.WithBookingService(booking.NewService(store)),
			httpapi.WithPaymentService(payment.NewService(store, []byte(cfg.paymentSecret))),
			httpapi.WithCancellationService(cancellation.NewServiceWithSecret(store, []byte(cfg.paymentSecret))),
			httpapi.WithFulfillmentService(fulfillment.NewService(store)),
			httpapi.WithReportingService(reporting.NewService(store)))
		server := newServer(cfg, handler)
		return server.ListenAndServeTLS(cfg.cert, cfg.key)
	}
	return errors.New("invalid mode")
}

func newServer(cfg config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func readConfirmedPassword(username string) ([]byte, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("password setup requires an interactive terminal")
	}
	return confirmPassword(username, func() ([]byte, error) {
		return term.ReadPassword(int(os.Stdin.Fd()))
	}, os.Stderr)
}

func confirmPassword(username string, read func() ([]byte, error), output io.Writer) ([]byte, error) {
	fmt.Fprintf(output, "Password for %s: ", username)
	password, err := read()
	fmt.Fprintln(output)
	if err != nil {
		return nil, err
	}
	fmt.Fprint(output, "Confirm password: ")
	confirmation, err := read()
	fmt.Fprintln(output)
	defer clear(confirmation)
	if err != nil || !bytes.Equal(password, confirmation) {
		clear(password)
		return nil, errors.New("password confirmation failed")
	}
	return password, nil
}
