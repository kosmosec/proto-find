# proto-find

proto-find is a tool for researchers that lets you find client side prototype pollution vulnerability.

# How it works

## proto-find open URL in Chrome using headless mode using [chromedp](https://github.com/chromedp/chromedp). 
> All you need is installed Chrome browser.

You have to provide JavaScript code in `-js` parameter which will be run in context of URL.

For the provided payload list (payloads.txt) the JavaScript code should be:
`window.elo`

## proto-find will process the URL in following way:

* if URL is `http://example.com/` the tool add payloads after "/", e.g. `http://example.com/?__proto__[elo]=melo`
* if URL has some parameters, e.g. `http://example.com/?name=test&work=hard&coffee=yes` the tool will inject the payload into each parameter, but one by one leaving the original values. See which requests will be executed:
    
    * http://example.com/?coffee=yes&name=test&work=hard&constructor.prototype.elo=melo
    * http://example.com/?coffee=yes&name=test&work=constructor.prototype.elo%3Dmelo
    * http://example.com/?coffee=yes&name=constructor.prototype.elo%3Dmelo&work=hard
    * http://example.com/?coffee=constructor.prototype.elo%3Dmelo&name=test&work=hard



# Installation

proto-find is written with Go and can be installed with `go get`:

```
▶ go get github.com/kosmosec/proto-find
```

Or you can clone the respository and build it manually:

```
▶ git clone https://github.com/kosmosec/proto-find.git
▶ cd proto-find
▶ go install
```

# Usage
## The concurrency (-c) 5 is the best for performance on regular computers. 

## Simple case
Run
```bash
csprotopollution -u ./urls -p ./payloads.txt -js window.elo -cookie "JSESSIONID=test;hello=world" -proxy "http://IP:PORT" -c 5
```
Output
```text
Vulnerable target http://<TARGET>/?name=test&work=hard&coffee=yes&__proto__[elo]={"json":"value"}
Vulnerable target http://<TARGET>/?name=test&work=hard&coffee=yes&__proto__[elo]=melo
Vulnerable target http://<TARGET>/?name=test&work=hard&coffee=yes&constructor[prototype][elo]=melo

```

----

Run 
```bash
cat urls | csprotopollution -p ./payloads.txt -js window.elo -cookie "JSESSIONID=test;hello=world" -proxy "http://IP:PORT" -c 5
```

## With custom headers
Copy headers from Burp Suite and paste to the file, e.x. 
```
X-Org: test
Auth: custom
```
Run
```bash
csprotopollution -u ./urls -p ./payloads.txt -h ./headers -js window.elo -cookie "JSESSIONID=test;hello=world"  -proxy "http://IP:PORT" -c 5
```


# Credits
* [Tomnomnom](https://github.com/tomnomnom) 
* [page-fetch](https://github.com/detectify/page-fetch)