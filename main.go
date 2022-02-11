package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"go.uber.org/ratelimit"
)

type options struct {
	javascript   string
	urlFile      string
	payloadsFile string
	cookie       string
	proxy        string
	headersFile  string
	version      bool
	concurrency  int
	rate         int
}

func main() {

	version := "0.1"

	opts := options{}

	flag.StringVar(&opts.javascript, "js", "", "JS script which check if prototype pollution exists")
	flag.StringVar(&opts.urlFile, "u", "", "file with URLs to scan")
	flag.StringVar(&opts.payloadsFile, "p", "", "file with client side prototype pollution payloads")
	flag.StringVar(&opts.headersFile, "h", "", "file with custom headers")
	flag.StringVar(&opts.cookie, "cookie", "", "set cookies, ex. -cookie \"session=hacker\"")
	flag.StringVar(&opts.proxy, "proxy", "", "set proxy for requests, -proxy \"http://192.168.0.241:8082\"")
	flag.IntVar(&opts.concurrency, "c", 10, "set concurrency")
	flag.BoolVar(&opts.version, "v", false, "version")
	flag.IntVar(&opts.rate, "rate", 0, "max rate for requests")
	flag.Parse()

	// if *proxyFlag != "" {
	// 	os.Setenv("HTTP_PROXY", *proxyFlag)
	// 	defer os.Setenv("HTTP_PROXY", "")
	// }

	if opts.version {
		fmt.Println(version)
		return
	}

	payloads, err := loadFileContent(opts.payloadsFile)
	fatalOnError(err)

	var urls []string

	if opts.urlFile != "" {
		urls, err = loadFileContent(opts.urlFile)
		fatalOnError(err)
	} else {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			u := sc.Text()
			urls = append(urls, u)
		}
	}

	var headers map[string]interface{}
	if opts.headersFile != "" {
		headersList, err := loadFileContent(opts.headersFile)
		fatalOnError(err)
		headers, err = processHeaders(headersList)
		fatalOnError(err)
	}

	var ratelimiter ratelimit.Limiter
	if opts.rate > 0 {
		ratelimiter = ratelimit.New(opts.rate)
	} else {
		ratelimiter = ratelimit.NewUnlimited()
	}

	var cookies []cookie
	if opts.cookie != "" {
		cookies, err = buildCookies(opts.cookie)
		fatalOnError(err)
	}

	chromedpOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("ignore-certificate-errors", true),
	)

	if opts.proxy != "" {
		_, err := url.ParseRequestURI(opts.proxy)
		if err != nil {
			log.Fatalln("invalid proxy URL")
		}
		chromedpOpts = append(chromedpOpts, chromedp.ProxyServer(opts.proxy))
	}

	ectx, ecancel := chromedp.NewExecAllocator(context.Background(), chromedpOpts...)
	defer ecancel()

	ctx, cancel := chromedp.NewContext(ectx)
	defer cancel()

	var wg sync.WaitGroup
	targets := make(chan string)

	for i := 0; i < opts.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for targetURL := range targets {
				urlParsed, err := url.Parse(targetURL)
				if err != nil {
					log.Printf("error on %s: %s", targetURL, err)
					continue
				}
				finalURLs := make(map[string]struct{})
				for _, payload := range payloads {
					urlWithPayload := buildURLWithPayload(urlParsed, payload)

					urlsWithPayloadInParam := buildURLsWithPayloadInParam(urlParsed, payload)
					if _, ok := finalURLs[urlWithPayload]; !ok {
						finalURLs[urlWithPayload] = struct{}{}
					}
					for _, u := range urlsWithPayloadInParam {
						if _, ok := finalURLs[u]; !ok {
							finalURLs[u] = struct{}{}
						}
					}
				}

				ctx, cancel := context.WithTimeout(ctx, time.Second*60)
				ctx, _ = chromedp.NewContext(ctx)
				for url := range finalURLs {
					ratelimiter.Take()
					scan(ctx, url, cookies, headers, opts.javascript)
				}
				cancel()
			}
		}()
	}

	for _, u := range urls {
		targets <- u
	}
	close(targets)
	wg.Wait()

}

func buildURLsWithPayloadInParam(targetUrl *url.URL, payload string) []string {

	origQuery := targetUrl.Query()

	urls := make([]string, 0)

	keys := make([]string, 0)
	for k := range origQuery {
		keys = append(keys, k)
	}
	for i := 0; i < len(origQuery); i++ {
		urlWithPayload, _ := url.Parse(targetUrl.String())
		values, _ := url.ParseQuery(urlWithPayload.RawQuery)
		values.Set(keys[i], payload[1:])
		urlWithPayload.RawQuery = values.Encode()
		urls = append(urls, urlWithPayload.String())

	}
	return urls
}

func buildURLWithPayload(targetUrl *url.URL, payload string) string {
	if len(targetUrl.Query()) == 0 {
		if payload[:1] != "&" {
			return fmt.Sprintf("%s%s", targetUrl.String(), payload)
		}
	}
	if payload[:1] != "?" {
		return fmt.Sprintf("%s%s", targetUrl.String(), payload)
	}
	return ""
}

type cookie struct {
	name  string
	value string
}

func buildCookies(cookieString string) ([]cookie, error) {
	multipleCookies := strings.Split(cookieString, ";")
	var cookies []cookie
	for _, c := range multipleCookies {
		splittedCookie, err := splitCookie(c)
		if err != nil {
			return nil, err
		}
		cookies = append(cookies, cookie{name: splittedCookie[0], value: splittedCookie[1]})
	}
	return cookies, nil
}

func splitCookie(c string) ([]string, error) {
	cookie := strings.Split(c, "=")
	if len(cookie) == 1 {
		return nil, fmt.Errorf("cookie %s in wrong format", c)
	}
	return cookie, nil
}

func scan(ctx context.Context, u string, cookies []cookie, headers map[string]interface{}, js string) {
	urlParsed, err := url.Parse(u)
	if err != nil {
		log.Printf("unable to parse URL %s", u)
		return
	}
	domain := urlParsed.Host

	var pollutionResult string
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			if cookies == nil {
				return nil
			}
			for _, c := range cookies {
				err := network.SetCookie(c.name, c.value).
					WithDomain(domain).
					Do(ctx)
				if err != nil {
					return err
				}
			}
			return nil
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if headers == nil {
				return nil
			}
			err := network.SetExtraHTTPHeaders(network.Headers(headers)).
				Do(ctx)
			if err != nil {
				return err
			}
			return nil
		}),
		emulation.SetUserAgentOverride("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/98.0.4758.82 Safari/537.36"),
		chromedp.Navigate(u),
		chromedp.EvaluateAsDevTools(js, &pollutionResult),
	)

	if err != nil {
		//log.Printf("chromedp.Run error: %s", err)
		return
	}

	fmt.Printf("Vulnerable target %s\n", u)
}

func loadFileContent(fileName string) ([]string, error) {
	raw, err := ioutil.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("read content from file %v: %w", fileName, err)
	}
	rawList := strings.Split(string(raw), "\n")
	contentList := make([]string, 0)
	for _, t := range rawList {
		if t != "" {
			contentList = append(contentList, strings.TrimSpace(t))
		}
	}

	return contentList, nil
}

func processHeaders(headerList []string) (map[string]interface{}, error) {
	headers := make(map[string]interface{})
	for _, h := range headerList {
		hName, hValue, err := splitHeader(h)
		if err != nil {
			return nil, err
		}
		headers[hName] = hValue
	}
	return headers, nil
}

func splitHeader(header string) (string, string, error) {
	splitted := strings.Split(header, ":")
	if len(splitted) == 1 {
		return "", "", fmt.Errorf("header %s has wrong format", header)
	}
	headerName := splitted[0]
	headerValue := splitted[1]
	return headerName, headerValue, nil

}

func fatalOnError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
