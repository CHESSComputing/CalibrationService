package main

import (
	"log"

	srvConfig "github.com/CHESSComputing/golib/config"
	server "github.com/CHESSComputing/golib/server"
	"github.com/CHESSComputing/golib/services"
	"github.com/gin-gonic/gin"
)

// Verbose defines verbosity level
var Verbose int

// global variables
var _foxdenUser services.UserAttributes

// helper function to setup our router
func setupRouter(store *Store) *gin.Engine {
	h := NewHandler(store)
	routes := []server.Route{
		{Method: "POST", Path: "", Handler: h.CreateCalibration, Authorized: true, Scope: "write"},
		{Method: "GET", Path: "/label/*label", Handler: h.ListCalibrations, Authorized: false, Scope: "read"},
		{Method: "GET", Path: "/valid/*label", Handler: h.GetValidCalibration, Authorized: true, Scope: "write"},
		{Method: "GET", Path: "/history/*label", Handler: h.GetHistory, Authorized: true, Scope: "write"},
		{Method: "PUT", Path: "/correct/*label", Handler: h.CorrectCalibration, Authorized: true, Scope: "write"},
		{Method: "DELETE", Path: "/iov/:id", Handler: h.DeleteIOV, Authorized: true, Scope: "read"},
		{Method: "DELETE", Path: "/label/*label", Handler: h.DeleteByLabel, Authorized: true, Scope: "write"},
	}
	r := server.Router(routes, nil, "static", srvConfig.Config.CalibrationData.WebServer)
	return r
}

// Server defines our HTTP server
func Server() {
	// init Verbose
	Verbose = srvConfig.Config.CalibrationData.WebServer.Verbose

	// make a choice of foxden user
	switch srvConfig.Config.CalibrationData.FoxdenUser.User {
	case "Maglab":
		_foxdenUser = &services.MaglabUser{}
	case "CHESS":
		_foxdenUser = &services.CHESSUser{}
	default:
		_foxdenUser = &services.CHESSUser{}
	}
	_foxdenUser.Init()

	dsn := srvConfig.Config.CalibrationData.DBUri
	if dsn == "" {
		log.Fatal("unable to get CalibrationData.DBUri foxden parameter")
	}

	store, err := NewStore(dsn)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer store.Close()

	// setup web router and start the service
	r := setupRouter(store)
	webServer := srvConfig.Config.CalibrationData.WebServer
	server.StartServer(r, webServer)
}
