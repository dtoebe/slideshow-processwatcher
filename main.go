package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-systemd/dbus"
	"github.com/vaughan0/go-ini"
)

type services struct {
	Service []service `json:"service"`
}

type service struct {
	Name   string `json:"name"`
	Status bool   `json:"status"`
}

func loadSettings() (core, services map[string]string) {
	file, err := ini.LoadFile("./settings.ini")
	if err != nil {
		log.Fatalf("[ERR] Connot load setting.ini file: %v\n", err)
	}
	return file["core"], file["services"]
}

func formatServiceSettings(data map[string]string) map[string][]string {
	services := make(map[string][]string)
	for key, val := range data {
		services[key] = strings.Split(val, ",")
	}
	return services
}

func startSystemConn() (conn *dbus.Conn) {
	conn, err := dbus.NewSystemdConnection()
	if err != nil {
		log.Fatalf("[ERR] Cannot create Systemd connection: %v\n", err)
	}
	return conn
}

func getUnits(conn *dbus.Conn) (units []dbus.UnitStatus) {
	units, err := conn.ListUnits()
	if err != nil {
		log.Fatalf("[ERR] Cannot get Systemd Unit List")
	}
	return units
}

func getStatues(name map[string]string, units []dbus.UnitStatus) (names map[string]string) {
	for _, unit := range units {
		for key, _ := range name {
			if unit.Name == key {
				name[key] = unit.ActiveState
			}
		}
	}

	return name
}

func startServiceWatcher(services map[string][]string, conn *dbus.Conn, core map[string]string) {
	status := make(chan map[string]string)
	data := make(chan []byte)
	duration, err := strconv.Atoi(core["check_duration"])
	if err != nil {
		log.Fatalf("[ERR] Error getting check_duration from settings.ini file: %v\n", err)
	}
	go serviceRoutine(services, conn, status, duration)
	for {
		go parseMapToJSON(<-status, data)
		go socketClient(core["sock_host"], core["sock_port"], data)
		fmt.Println(<-status)
	}
}

func serviceRoutine(services map[string][]string, conn *dbus.Conn, servChan chan map[string]string, delay int) {
	servStatus := make(map[string]string)
	fmt.Println(services)
	for {
		units := getUnits(conn)
		for i := 0; i < len(units); i++ {
			for _, val := range services {
				if units[i].Name == val[0] {
					servStatus[val[0]] = units[i].ActiveState
				}
			}
			if i == len(units)-1 {
				for _, val := range services {
					if len(servStatus[val[0]]) == 0 {
						servStatus[val[0]] = "innactive"
					}
				}
			}
		}
		servChan <- servStatus
		servStatus = make(map[string]string)
		time.Sleep(time.Duration(delay) * time.Second)
		runtime.Gosched()
	}
}

func socketClient(host, port string, data <-chan []byte) {
	conn, err := net.Dial("tcp", host+":"+port)
	if err != nil {
		log.Printf("[ERR] unable to dial out to the webserver UNIX socket server")
		return
	}
	conn.Write(<-data)
	defer conn.Close()
}

func parseMapToJSON(servStatus map[string]string, data chan []byte) {

	aServ := make([]service, len(servStatus), len(servStatus))
	count := 0

	for key, val := range servStatus {
		var bVal bool
		if val == "innactive" {
			bVal = false
		} else {
			bVal = true
		}
		aServ[count] = service{Name: key, Status: bVal}
		count++
	}

	s := services{Service: aServ}

	j, err := json.Marshal(&s)
	if err != nil {
		log.Printf("[ERR] Fialed to marshal json: %v\n", err)
		return
	}

	data <- j

}

func main() {
	core, services := loadSettings()
	servs := formatServiceSettings(services)
	conn := startSystemConn()
	startServiceWatcher(servs, conn, core)
}
