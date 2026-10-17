package urlmask_test

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/sikhlana/pkg/url/urlmask"
)

func ExampleURL() {
	u, err := url.Parse(
		"https://alice:hunter2@example.com/app/cart;jsessionid=8F3A1C?token=abc&page=2#access_token=SECRET",
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(urlmask.URL(u))
	// Output: https://alice:xxxxx@example.com/app/cart;jsessionid=***?token=***&page=***
}

func ExampleRaw() {
	fmt.Println(urlmask.Raw("https://example.com/reset?eyJhbGciOiJIUzI1NiJ9.payload.sig&lang=en"))
	fmt.Println(urlmask.Raw("mailto:alice@example.com"))
	fmt.Println(urlmask.Raw("https://app example.com/?token=abc"))
	// Output:
	// https://example.com/reset?***&lang=***
	// mailto:***
	// ***
}

func ExampleValue() {
	u, err := url.Parse("https://example.com/search?q=secret")
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}

			return a
		},
	}))

	logger.Info("request", slog.Any("url", urlmask.Value(u)))
	// Output: level=INFO msg=request url="https://example.com/search?q=***"
}
