package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/context"
	"strconv"
)

type sliceVar []string
type hostFlagsVar []string

type Context struct {
}

func (c *Context) Env() map[string]string {
	env := make(map[string]string)
	for _, i := range os.Environ() {
		sep := strings.Index(i, "=")
		env[i[0:sep]] = i[sep+1:]
	}
	return env
}

var (
	buildVersion string
	version      bool
	poll         bool
	wg           sync.WaitGroup

	templatesFlag   sliceVar
	stdoutTailFlag  sliceVar
	stderrTailFlag  sliceVar
	delimsFlag      string
	delims          []string
	waitFlag        hostFlagsVar
	waitTimeoutFlag time.Duration
	dependencyChan  chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
)

func (i *hostFlagsVar) String() string {
	return fmt.Sprint(*i)
}

func (i *hostFlagsVar) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func (s *sliceVar) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *sliceVar) String() string {
	return strings.Join(*s, ",")
}

func waitForDependencies() {
	dependencyChan := make(chan struct{})

	go func() {
		for _, host := range waitFlag {
			log.Println("Waiting for host:", host)
			u, err := url.Parse(host)
			if err != nil {
				log.Fatalf("bad hostname provided: %s. %s", host, err.Error())
			}

			switch u.Scheme {
			case "tcp", "tcp4", "tcp6":
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						conn, _ := net.DialTimeout(u.Scheme, u.Host, waitTimeoutFlag)
						if conn != nil {
							log.Println("Connected to", u.String())
							return
						}
					}
				}()
			case "http", "https":
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						resp, err := http.Get(u.String())
						if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
							log.Printf("Received %d from %s\n", resp.StatusCode, u.String())
							return
						}
					}
				}()
			default:
				log.Fatalf("invalid host protocol provided: %s. supported protocols are: tcp, tcp4, tcp6 and http", u.Scheme)
			}
		}
		wg.Wait()
		close(dependencyChan)
	}()

	select {
	case <-dependencyChan:
		break
	case <-time.After(waitTimeoutFlag):
		log.Fatalf("Timeout after %s waiting on dependencies to become available: %v", waitTimeoutFlag, waitFlag)
	}

}

func usage() {
	println(`Usage: dockerize [options] [command]

Utility to simplify running applications in docker containers

Options:`)
	flag.PrintDefaults()

	println(`
Arguments:
  command - command to be executed
  `)

	println(`Examples:
`)
	println(`   Generate /etc/nginx/nginx.conf using nginx.tmpl as a template, tail /var/log/nginx/access.log
   and /var/log/nginx/error.log, waiting for a website to become available on port 8000 and start nginx.`)
	println(`
   dockerize -template nginx.tmpl:/etc/nginx/nginx.conf \
             -stdout /var/log/nginx/access.log \
             -stderr /var/log/nginx/error.log \
             -wait tcp://web:8000 nginx
	`)

	println(`For more information, see https://github.com/jwilder/dockerize`)
}

func main() {

	flag.BoolVar(&version, "version", false, "show version")
	flag.BoolVar(&poll, "poll", false, "enable polling")
	flag.Var(&templatesFlag, "template", "Template (/template:/dest). Can be passed multiple times")
	flag.Var(&stdoutTailFlag, "stdout", "Tails a file to stdout. Can be passed multiple times")
	flag.Var(&stderrTailFlag, "stderr", "Tails a file to stderr. Can be passed multiple times")
	flag.StringVar(&delimsFlag, "delims", "", `template tag delimiters. default "{{":"}}" `)
	flag.Var(&waitFlag, "wait", "Host (tcp/tcp4/tcp6/http/https) to wait for before this container starts. Can be passed multiple times. e.g. tcp://db:5432")
	flag.DurationVar(&waitTimeoutFlag, "timeout", 10*time.Second, "Host wait timeout")

	flag.Usage = usage
	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	if flag.NArg() == 0 && flag.NFlag() == 0 {
		usage()
		os.Exit(1)
	}

	// detect what port should use
	portNumStr := GetEnv("NEED_PORTS")
	server := GetEnv("K8S_API_SERVER_ADDR")
	if len(portNumStr) > 0 {
		portNum, err := strconv.Atoi(portNumStr)
		if err == nil && portNum > 0 {
			neededPorts, err := GetAvailablePorts(portNum)
			if err != nil {
				log.Fatalf("can't get available port:", err.Error())
				os.Exit(5)
			}
			fmt.Println("get ports:", neededPorts)
			portEnvs := FormatPortEnv(neededPorts)
			ExportEnvs(portEnvs)
			err = ReportInfos(server, portEnvs)
			if err != nil {
				log.Fatalf("can't report port to kubenetes:", err.Error())
				os.Exit(6)
			}
		} else {
			log.Fatalf("can't parse $NEED_PORTS:", err.Error())
		}
	}

	if delimsFlag != "" {
		delims = strings.Split(delimsFlag, ":")
		if len(delims) != 2 {
			log.Fatalf("bad delimiters argument: %s. expected \"left:right\"", delimsFlag)
		}
	}
	templateEnv := GetEnv("DOMEIZE_TEMPLATES")
	var templates sliceVar
	if (len(templateEnv) > 0) {
		if strings.Contains(templateEnv, ",") {
			templates = strings.Split(templateEnv, ",")
		} else {
			templates = append(templates, templateEnv)
		}
	}
	for _, t := range templates {
		templatesFlag = append(templatesFlag, t)
	}
	for _, t := range templatesFlag {
		template, dest := t, ""
		if strings.Contains(t, ":") {
			parts := strings.Split(t, ":")
			if len(parts) != 2 {
				log.Fatalf("bad template argument: %s. expected \"/template:/dest\"", t)
			}
			template, dest = parts[0], parts[1]
		}
		generateFile(template, dest)
	}

	waitForDependencies()

	// Setup context
	ctx, cancel = context.WithCancel(context.Background())

	if flag.NArg() > 0 {
		wg.Add(1)
		go runCmd(ctx, cancel, flag.Arg(0), flag.Args()[1:]...)
	}

	for _, out := range stdoutTailFlag {
		wg.Add(1)
		go tailFile(ctx, out, poll, os.Stdout)
	}

	for _, err := range stderrTailFlag {
		wg.Add(1)
		go tailFile(ctx, err, poll, os.Stderr)
	}

	wg.Wait()
}
