package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type podSpec struct {
	Metadata metadatast
}

type metadatast struct {
	Annotations map[string]string
}

func getPatchUrl(server string) (string, error) {
	ns := GetEnv("MY_POD_NAMESPACE")
	name := GetEnv("MY_POD_NAME")
	if len(ns) == 0 || len(name) == 0 || len(server) == 0 {
		log.Fatalf("failed to get url:namespace:%s, podname:%s, k8sserver:%s", ns, name, server)
		return "", errors.New("failed to get k8s api server")
	}
	return fmt.Sprintf("http://%s/api/v1/namespaces/%s/pods/%s", server, ns, name), nil
}

func patchInfo(apiAddr string, body io.Reader) error {
	req, err := http.NewRequest("PATCH", apiAddr, body)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/strategic-merge-patch+json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New("report port not ok")
	}
	return nil
}

func ReportInfos(server string, portEnvs map[string]string) error {

	st := podSpec{metadatast{portEnvs}}
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}

	url, err := getPatchUrl(server)
	if err != nil {
		return err
		err.Error()
	}
	s := string(body)

	return patchInfo(url, strings.NewReader(s))
}

//func main() {
//	envs := make(map[string]string)
//	envs["conan"] = "tesdkf"
//	ReportInfos(envs)
//}

//PATCH /api/v1/namespaces/{namespace}/pods/{name}
