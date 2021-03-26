package main

import (
	"context"
	"log"

	se "github.com/Snawoot/opera-proxy/seclient"
)

const (
	username = "se0316"
	password = "SILrMEPBmJuhomxWkfm3JalqHX2Eheg1YhlEZiMh8II"
)

func main() {
	seclient, err := se.NewSEClient(username, password, nil)
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("seclient = %#v", seclient)

	log.Println("------------ DOING REGISTRATION ------------")
	err = seclient.AnonRegister(context.TODO())
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("seclient = %#v", seclient)
	log.Printf("jar = %#v", seclient.HttpClient.Jar)

	log.Println("------------ DOING DEVICE REGISTRATION ------------")
	err = seclient.RegisterDevice(context.TODO())
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("seclient = %#v", seclient)
	log.Printf("Device Password: %s", seclient.DevicePassword)

	log.Println("------------ GETTING GEO LIST ------------")
	geos, err := seclient.GeoList(context.TODO())
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("Geo List: %#v", geos)

	log.Println("------------ GETTING IP LIST ------------")
	ips, err := seclient.Discover(context.TODO(), "\"EU\",,")
	if err != nil {
		log.Fatalln(err)
	}
	log.Printf("IP List: %#v", ips)
}
