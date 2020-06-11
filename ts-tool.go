package main

import (
    "fmt"
    "net/http"
    "crypto/tls"
    "log"
    "io/ioutil"
    "bytes"
    "os"
    "time"
    "strings"
    // "path/filepath"
    "github.com/tidwall/gjson"
    "encoding/json"
    "encoding/base64"
    "flag"
    "sync"
)

type VerifiedTSInfo struct {
    NodeVersion string
    Version string
    Release string
    SchemaCurrent string
    SchemaMinimum string
}

type Schedule struct {
    Targets []string
    Credential string
    Template string
    Version string
}

type Package struct {
    Version string
    Remotepath string
    Filepath string
    Sha256sum string
    Name string
    Release string
    Arch string
    PackageName string
}

type Target struct {
    Ipaddr string
    Credential string
}

const E = "ERROR"
const I = "INFO"
const W = "WARN"
const D = "DEBUG"

// max cocurrent data transformation 
var translock = make(chan bool, 2)

// summary writing lock in goroutine
var mutex = &sync.Mutex{}

// use sync.WaitGroup to wait goroutines.
var wg sync.WaitGroup

func LogMsg(logLevel string, tgt *Target, msg string) string {
    raw := fmt.Sprintf("[%s] Target %s: %s", logLevel, tgt.Ipaddr, msg)
    log.Print(raw)
    return raw
}

func ErrMsg(tgt *Target, msg string) error {
    return fmt.Errorf(LogMsg(E, tgt, msg))
}

func Upload(client *http.Client, tgt *Target, pkg *Package) error {

    ps := strings.Split(pkg.Filepath, "/")
    pkgName := ps[len(ps)-1]

    // TODO: use absolute path
    pkgPath := pkg.Filepath
    finfo, err := os.Stat(pkgPath)
    if os.IsNotExist(err) {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to get filestat %s: %s", pkgPath, err.Error()),
        )
    }

    // TODO: check file's sha256sum

    uploadBody, err := ioutil.ReadFile(pkgPath)
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to read file %s: %s", pkgPath, err.Error()),
        )
    }

    uploadUrl := fmt.Sprintf(
        `https://%s/mgmt/shared/file-transfer/uploads/%s`, 
        tgt.Ipaddr,
        pkgName,
    )

    reqUpload, err := http.NewRequest(
        "POST", 
        uploadUrl, 
        bytes.NewReader(uploadBody),
    )
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to new upload request: %s", err.Error()),
        )
    }
    // TODO: use sched.Credential
    reqUpload.Header.Add("Authorization", tgt.Credential)
    reqUpload.Header.Add("Content-Type", "application/octet-stream")
    reqUpload.Header.Add(
        "Content-Range", 
        fmt.Sprintf(`0-%d/%d`, finfo.Size()-1, finfo.Size()),
    )
    reqUpload.Header.Add("Content-Length", string(finfo.Size()))
    reqUpload.Header.Add("Connection", "keep-alive")

    LogMsg(I, tgt, "Uploading package ...")
    respUpload, err := client.Do(reqUpload)
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to request %s: %s", uploadUrl, err.Error()),
        )
    }

    defer respUpload.Body.Close()

    if int(respUpload.StatusCode / 200) != 1 {
        rsp, _ := ioutil.ReadAll(respUpload.Body)
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to upload: %d, %s", respUpload.StatusCode, rsp),
        )
    } else {
        LogMsg(I, tgt, fmt.Sprintf("uploaded package %s", pkgName))
    }

    return nil
}

func Install(client *http.Client, tgt *Target, pkg *Package) error {
    ps := strings.Split(pkg.Filepath, "/")
    pkgName := ps[len(ps)-1]

    installUrl := fmt.Sprintf(
        "https://%s/mgmt/shared/iapp/package-management-tasks",
        tgt.Ipaddr,
    )
    installBody := fmt.Sprintf(`{
            "operation": "INSTALL",
            "packageFilePath": "/var/config/rest/downloads/%s"
        }`, 
        pkgName,
    )

    reqInstall, err := http.NewRequest(
        "POST",
        installUrl,
        strings.NewReader(installBody),
    )
    if err != nil {
        return ErrMsg(tgt, 
            fmt.Sprintf("failed to new install request: %s", err.Error()),
        )
    }

    reqInstall.Header.Add("Content-Type", "application/json;charset=UTF-8")
    reqInstall.Header.Add("Authorization", tgt.Credential)
    
    respInstall, err := client.Do(reqInstall)
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to request %s: %s", installUrl, err.Error()),
        )
    }
    defer respInstall.Body.Close()

    if int(respInstall.StatusCode / 200) != 1 {
        rsp, _ := ioutil.ReadAll(respInstall.Body)
        return ErrMsg(
            tgt, 
            fmt.Sprintf(
                "failed to install: %d, %s", respInstall.StatusCode, rsp,
            ),
        )
    } else {
        LogMsg(I, tgt, fmt.Sprintf("installed TS package %s", pkgName))
    }

    return nil
}

func Verify(client *http.Client, tgt *Target) (int, *VerifiedTSInfo, error){

    verifyUrl := fmt.Sprintf(
        `https://%s/mgmt/shared/telemetry/info`, tgt.Ipaddr,
    )

    reqVerify, err := http.NewRequest("GET", verifyUrl, nil)
    if err != nil {
        return 0, nil, ErrMsg(
            tgt, 
            fmt.Sprintf("failed to new verify request: %s", err.Error()),
        )
    }

    reqVerify.Header.Add("Authorization", tgt.Credential)
    
    respVerify, err := client.Do(reqVerify)
    if err != nil {
        return 0, nil, ErrMsg(
            tgt, 
            fmt.Sprintf("failed to request %s: %s", verifyUrl, err.Error()),
        )
    }
    defer respVerify.Body.Close()

    vts := VerifiedTSInfo{}
    verified, _ := ioutil.ReadAll(respVerify.Body)
    if respVerify.StatusCode != 200 {
        return respVerify.StatusCode, nil, nil
    } else {
        LogMsg(I, tgt, fmt.Sprintf("TS info: %s", string(verified)))
        err := json.Unmarshal(verified, &vts)
        if err != nil {
            return respVerify.StatusCode, nil, ErrMsg(tgt, fmt.Sprintf(
                "failed to get ts info from response: %s, %s", err, verified,
            ))
        }
    }

    return 200, &vts, nil;
}

func Deploy(client *http.Client, tgt *Target, declaration []byte) error {
    deployUrl := fmt.Sprintf(
        "https://%s/mgmt/shared/telemetry/declare", tgt.Ipaddr,
    )

    reqDeploy, err := http.NewRequest(
        "POST", deployUrl, bytes.NewReader(declaration),
    )
    if err != nil {
        return ErrMsg(tgt, 
            fmt.Sprintf("failed to new deploy request: %s", err.Error()),
        )
    }

    reqDeploy.Header.Add("Authorization", tgt.Credential)

    respDeploy, err := client.Do(reqDeploy)
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to POST %s: %s", deployUrl, err.Error()),
        )
    }

    defer respDeploy.Body.Close()

    if int(respDeploy.StatusCode / 200) != 1 {
        reason, _ := ioutil.ReadAll(respDeploy.Body)
        return ErrMsg(
            tgt, 
            fmt.Sprintf(
                "deploy template POST %s: %d, %s.", 
                deployUrl, respDeploy.StatusCode, reason,
            ),
        )
    } else {
        LogMsg(I, tgt, fmt.Sprintf("deployed template successfully"))
    }

    return nil
}

func GetInstalledPkgs(client *http.Client, tgt *Target) ([]Package, error) {
    queryUrl := fmt.Sprintf(
        "https://%s/mgmt/shared/iapp/package-management-tasks",
        tgt.Ipaddr,
    )

    queryBody := []byte(`{ "operation": "QUERY" }`)
    reqGetPkgs, err := http.NewRequest(
        "POST", queryUrl, bytes.NewReader(queryBody),
    )
    if err != nil {
        return []Package{}, ErrMsg(tgt, 
            fmt.Sprintf("failed to new task query request: %s", err.Error()),
        )
    }

    reqGetPkgs.Header.Add("Authorization", tgt.Credential)

    respGetPkgs, err := client.Do(reqGetPkgs)
    if err != nil {
        return []Package{}, ErrMsg(
            tgt, 
            fmt.Sprintf("failed to POST %s: %s", queryUrl, err.Error()),
        )
    }

    defer respGetPkgs.Body.Close()
    bd, _ := ioutil.ReadAll(respGetPkgs.Body)

    if int(respGetPkgs.StatusCode / 200) != 1 {    
        return []Package{}, ErrMsg(
            tgt, fmt.Sprintf(
                "Get Pkgs POST %s - response code %d", 
                queryUrl, respGetPkgs.StatusCode,
        ))    
    }

    queryid := gjson.GetBytes(bd, "id")

    code, rbd, err := ResultOfPkgMgmtTask(client, tgt, queryid.Str)
    if code != 200 {
        // TODO logging it.
        return []Package{}, ErrMsg(tgt, fmt.Sprintf(
            "failed to get result %d", code,
            ))
    } else {
        pkgsBytes := gjson.GetBytes(rbd, "queryResponse")

        var pkgList []Package
        e := json.Unmarshal([]byte(pkgsBytes.Raw), &pkgList)
        if e != nil {
            return []Package{}, ErrMsg(
                tgt, 
                fmt.Sprintf(
                    "failed to parse pkg list from response: %s: %s", 
                    e.Error(), pkgsBytes,
                ),
            )
        }
    
        return pkgList, nil
    }
}

func ResultOfPkgMgmtTask(
    client *http.Client, tgt *Target, taskId string,
) (int, []byte, error) {
    taskUrl := fmt.Sprintf(
        "https://%s/mgmt/shared/iapp/package-management-tasks/%s",
        tgt.Ipaddr, taskId,
    )

    reqTask, err := http.NewRequest("GET", taskUrl, nil)
    if err != nil {
        return 0, []byte{}, ErrMsg(tgt, 
            fmt.Sprintf("failed to new install request: %s", err.Error()),
        )
    }

    reqTask.Header.Add("Authorization", tgt.Credential)

    respTask, err := client.Do(reqTask)
    if err != nil {
        return 0, []byte{}, ErrMsg(
            tgt, 
            fmt.Sprintf("failed to GET %s: %s", taskUrl, err.Error()),
        )
    }
    defer respTask.Body.Close()

    bd, _ := ioutil.ReadAll(respTask.Body)
    return respTask.StatusCode, bd, nil
}

func Uninstall(client *http.Client, tgt *Target) error {
    LogMsg(I, tgt, "Uninstalling...")

    pkgList, err := GetInstalledPkgs(client, tgt)
    if err != nil {
        return ErrMsg(
            tgt, 
            fmt.Sprintf("failed to uninstall pkg: %s", err.Error()),
        )
    }

    for _, pkg := range pkgList {
        if pkg.Name == "f5-telemetry" {
            uninstallUrl := fmt.Sprintf(
                "https://%s/mgmt/shared/iapp/package-management-tasks",
                tgt.Ipaddr,
            )
            uninstallBody := fmt.Sprintf(`
                {
                    "operation": "UNINSTALL",
                    "packageName": "%s"
                }
                `, pkg.PackageName)
            uninstallReq, err := http.NewRequest(
                "POST", uninstallUrl, strings.NewReader(uninstallBody),
            )
            if err != nil {
                return ErrMsg(tgt, 
                    fmt.Sprintf(
                        "failed to new uninstall request: %s", err.Error(),
                    ),
                )
            }

            uninstallReq.Header.Add("Authorization", tgt.Credential)

            respUninstall, err := client.Do(uninstallReq)
            if err != nil {
                return ErrMsg(
                    tgt, 
                    fmt.Sprintf(
                        "failed to POST %s: %s", 
                        uninstallUrl, err.Error(),
                    ),
                )
            }
        
            defer respUninstall.Body.Close()

            bd, _ := ioutil.ReadAll(respUninstall.Body)
            if int(respUninstall.StatusCode / 200) != 1 {
                return ErrMsg(
                    tgt, 
                    fmt.Sprintf(
                        "uninstall POST %s - response code: %d, %s", 
                        uninstallUrl, respUninstall.StatusCode, bd,
                    ),
                )
            }

            LogMsg(
                I, tgt, 
                fmt.Sprintf("uninstalled package %s", pkg.PackageName),
            )
            // TODO: check uninstall status
            // taskId := gjson.GetBytes(bd, "status")
            // ResultOfPkgMgmtTask(client, tgt, taskId.Str)
            break
        }
    }

    return nil
}

// TODO: Undeploy

func UpdateStatus(
    mapping map[string][]string, 
    key string, 
    value string) {

    mutex.Lock()
    if _, ok := mapping[key]; !ok {
        mapping[key] = []string{}
    }

    mapping[key] = append(mapping[key], value)
    mutex.Unlock()
}

func Setup(
    execnum chan string,
    client *http.Client, 
    tgt *Target, 
    pkg *Package, 
    t []byte,
    rlt map[string][]string,
) error {

    execnum <- tgt.Ipaddr
    defer func() {
        <- execnum
        wg.Done()
    }()

    code, vts, err := Verify(client, tgt)
    if err != nil && code == 0 {
        UpdateStatus(rlt, tgt.Ipaddr, "verify: x")
        return ErrMsg(tgt, fmt.Sprintf("setup: failed to verify: %s", err))
    } else {
        UpdateStatus(rlt, tgt.Ipaddr, "verify: y")
    }
    
    if vts == nil || vts.Version != pkg.Version {
        LogMsg(
            I, 
            tgt,
            fmt.Sprintf("TS version is not %s, installing ...", pkg.Version),
        )

        translock <- true
        dur := client.Timeout
        client.Timeout = 240 * time.Second
        err = Upload(client, tgt, pkg)
        client.Timeout = dur
        <- translock

        if err != nil {
            UpdateStatus(rlt, tgt.Ipaddr, "upload: x")
            return ErrMsg(tgt, fmt.Sprintf("setup: failed to upload: %s", err))
        }
        UpdateStatus(rlt, tgt.Ipaddr, "upload: y")
        err = Install(client, tgt, pkg)
        if err != nil {
            UpdateStatus(rlt, tgt.Ipaddr, "install: x")
            return ErrMsg(tgt, fmt.Sprintf("setup: failed to install: %s", err))
        }
        UpdateStatus(rlt, tgt.Ipaddr, "install: y")
    } else {
        UpdateStatus(rlt, tgt.Ipaddr, "upload: -")
        UpdateStatus(rlt, tgt.Ipaddr, "install: -")
        LogMsg(
            I, tgt, fmt.Sprintf("TS version matched %s, skip.", pkg.Version),
        )
    }

    if len(t) != 0 {
        i := 0
        wait := 20
        for i < wait {
            code, vts, err = Verify(client, tgt)
            if vts != nil && vts.Version == pkg.Version {
                break
            }
            time.Sleep(1 * time.Second)
            i ++
        }
        if i == wait {
            UpdateStatus(rlt, tgt.Ipaddr, "check: x")
            return ErrMsg(
                tgt, 
                fmt.Sprintf(
                    "setup: TS endpoint not available, waiting timeout.",
                ),
            )
        }
        UpdateStatus(rlt, tgt.Ipaddr, "check: y")

        err := Deploy(client, tgt, t)
        if err != nil {
            UpdateStatus(rlt, tgt.Ipaddr, "deploy: x")
            return ErrMsg(tgt, fmt.Sprintf("setup: failed to deploy: %s", err))
        }
        UpdateStatus(rlt, tgt.Ipaddr, "deploy: y")
    } else {
        UpdateStatus(rlt, tgt.Ipaddr, "deploy: -")
        LogMsg(I, tgt, fmt.Sprintf("skips deploying TS declaration."))
    }

    return nil
}

func Teardown(
    execnum chan string,
    client *http.Client, 
    tgt *Target, 
    rlt map[string][]string,
) error {
    execnum <- tgt.Ipaddr
    defer func() {
        <- execnum
        wg.Done()
    }()

    err := Uninstall(client, tgt)
    if err != nil {
        UpdateStatus(rlt, tgt.Ipaddr, "uninstall: x")
        return ErrMsg(
            tgt, fmt.Sprintf("teardown: failed to uninstall: %s", err),
        )
    } else {
        UpdateStatus(rlt, tgt.Ipaddr, "uninstall: y")
        return nil
    }
}

func NewClient() *http.Client {
    tr :=  &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    }
    client := http.Client{
        Transport: tr,
        Timeout: time.Duration(30 * time.Second),
    }

    return &client
}

func PackageOf(confCnt []byte, name string) *Package {
    bpkg := gjson.GetBytes(
        confCnt, 
        fmt.Sprintf(
            "packages.%s", 
            strings.ReplaceAll(name, ".", "\\."),
        ),
    )
    if !bpkg.Exists() {
        panic(fmt.Errorf("packge of name %s doesn't exist", name))
    }
    var pkg Package
    e := json.Unmarshal([]byte(bpkg.Raw), &pkg)
    if e != nil {
        panic(fmt.Errorf("package format error: %s, %s", bpkg.Raw, e))
    }

    pkg.Version = name

    return &pkg
}

func TemplateOf(confCnt []byte, name string) []byte {
    if name == "" {
        return []byte{}
    }

    tmpl := gjson.GetBytes(
        confCnt, 
        fmt.Sprintf(
            "templates.%s", 
            strings.ReplaceAll(name, ".", "\\."),
        ),
    )
    if !tmpl.Exists() {
        panic(
            fmt.Errorf("template %s not defined.", name),
        )
    }
    
    return []byte(tmpl.Raw)
}

func TargetOf(ipaddr string, cred string) *Target {
    b64 := base64.StdEncoding.EncodeToString([]byte(cred))
    tgt := Target{
        Ipaddr: ipaddr,
        Credential: fmt.Sprintf("Basic %s", b64),
    }
    return &tgt
}

func main() {
    fmt.Println("Setting up Telemetry Streaming on BIG-IP ...")

	var destroy bool
    var cocurrency int
    var configFile string
	flag.BoolVar(&destroy, "d", false, "Uninstall for all targets.")
    flag.IntVar(&cocurrency, "n", 3, "Cocurrent execution count.")
    flag.StringVar(
        &configFile, "f", "./ts-settings.json", "Configuration for execution.")
    flag.Parse()

    // dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
    // panic(err)
    // path := filepath.Join(dir, "ts-settings.json")
    // TODO: use absolute path to ts-settings.json
    path := "./ts-settings.json"

    confCnt, err := ioutil.ReadFile(path)
    if err != nil {
        panic(err)
    }
    if !gjson.Valid(string(confCnt)) {
        panic(fmt.Errorf("Invalid json format: %s", path))
    }

    var schedules []Schedule
    d := gjson.GetBytes(confCnt, "schedules")
    e := json.Unmarshal([]byte(d.Raw), &schedules)
    if e != nil {
        panic(e)
    }

    // max cocurent execution
    execnum := make(chan string, cocurrency)
    defer close(execnum)
    result := map[string][]string{}

    A := func () {
        for _, s := range schedules {
            // TODO: removing duplicate ipaddrs in s.Targets
            // TODO: expending ipaddr range like: 10.145.74.75-79 
            for _, ipaddr := range s.Targets {

                client := NewClient()
                i := TargetOf(ipaddr, s.Credential)
                p := PackageOf(confCnt, s.Version)
                t := TemplateOf(confCnt, s.Template)

                wg.Add(1)
                if !destroy {
                    go Setup(execnum, client, i, p, t, result)
                } else {
                    go Teardown(execnum, client, i, result)
                }
            }
        }
    }

    B := func () {
        wg.Wait()
        fmt.Printf("\nRunning Summary\n\n")
        for k, v := range result {
            fmt.Printf("%-18s : %v\n", k, v)
        }
        fmt.Printf("\n")
    }

    A()
    B()
}
