// Author: Daniel Toebe <dtoebe@gmail.com>
//License: All code I have written is licensed under MIT
//	All other code I have not written such as dependacies are licensed under thier
//	respecive License

//slideshow-process watcher: uses dbus to watch services in Systemd
//systemd services defined in the settings.ini
//this file will be used under root
//settings.ini should be plces in the same dir as the compiled binary
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

//services is a struct used to structure the data to be sent to the webserver
type services struct {
	Service []service `json:"service"`
}

//service is the structure of data each watched service to be sent to the webserver
type service struct {
	Name   string `json:"name"`
	Status bool   `json:"status"`
}

//loadSettings loads the settings.ini file and returns the core amd images section
//	as map[string]string
func loadSettings() (core, services map[string]string) {
	file, err := ini.LoadFile("./settings.ini")
	if err != nil {
		log.Fatalf("[ERR] Connot load setting.ini file: %v\n", err)
	}
	return file["core"], file["services"]
}

//formatServiceSettings takes the val string in a map[string]string and seperates
//	it at "," and returns it as a the map as a map[string][]string
func formatServiceSettings(data map[string]string) map[string][]string {
	services := make(map[string][]string)
	for key, val := range data {
		services[key] = strings.Split(val, ",")
	}
	return services
}

//startSystemConn starts watching dbus and returns its connection as a dbus.Conn leteral
func startSystemConn() (conn *dbus.Conn) {
	conn, err := dbus.NewSystemdConnection()
	if err != nil {
		log.Fatalf("[ERR] Cannot create Systemd connection: %v\n", err)
	}
	return conn
}

//getUnits takes the dbus.Conn lteral ant collects the entire list of systemd services
//	returns an array of []dbus.UnitStatus
func getUnits(conn *dbus.Conn) (units []dbus.UnitStatus) {
	units, err := conn.ListUnits()
	if err != nil {
		log.Fatalf("[ERR] Cannot get Systemd Unit List")
	}
	return units
}

//getStatues takes the wanted services (map[string]string) and the list of services
//	collected ([]dbus.UnitStatus) then returns the needed status ActiveState as
//	map[string]string
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

//startServiceWatcher takes the wanted services (map[string][]string) the connection (*dbus.Conn)
//	and the core info from the settings file (map[string]string) starts serviceRoutine
//	as a goroutine to activly checking the status of the wanted service, also
//	starts an infinate loop and structures the returned data from serviceRoutine
//	and sends it to the websever
func startServiceWatcher(services map[string][]string, conn *dbus.Conn, core map[string]string) {
	//NOTE: Does this func do to much?
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

//serviceRoutine takes the wanted services (map[string][]string), chan for the state
//	of the wanted services (chan map[string]string) and the delay to block the loop (int)
//	in seconds
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

//socketClient simple tcp socket server that only sends data, not receive because
//	this script runs as root user: takes the host and port (string) and data (<-chan []byte)
func socketClient(host, port string, data <-chan []byte) {
	conn, err := net.Dial("tcp", host+":"+port)
	if err != nil {
		log.Printf("[ERR] unable to dial out to the webserver UNIX socket server")
		return
	}
	conn.Write(<-data)
	defer conn.Close()
}

//parseMapToJSON takes the statuses of the wanted services and a data chan (chan []byte)
//	converts it the services struct and json.Marshal and send out the []byte
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
