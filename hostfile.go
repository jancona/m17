package m17

import (
	"encoding/csv"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
)

type Hostfile struct {
	Hosts map[string]Host
}

func NewHostfile(name string) (*Hostfile, error) {
	log.Printf("[DEBUG] Loading hostfile: %s", name)
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("unable to open hostfile %s: %v", name, err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = ' '
	r.Comment = '#'
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unable to read hostfile %s: %v", name, err)
	}
	ret := &Hostfile{
		Hosts: make(map[string]Host),
	}
	if len(recs) > 0 && len(recs[0]) == 1 {
		// if the first record only has one field, we're using the wrong delimiter
		// so try again
		log.Print("[INFO] Detected wrong hostfile delimiter, trying tab instead of space")
		f.Close()
		f, err = os.Open(name)
		if err != nil {
			return nil, fmt.Errorf("unable to open hostfile %s: %v", name, err)
		}
		r = csv.NewReader(f)
		r.Comma = '\t'
		r.Comment = '#'
		recs, err = r.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("unable to read hostfile %s: %v", name, err)
		}
	}
	for _, rec := range recs {
		server := rec[1]
		ip := net.ParseIP(rec[1])
		if ip != nil {
			if ip.To4() == nil {
				// it's IPv6, so wrap it in brackets
				server = "[" + rec[1] + "]"
			}
		}
		port, err := strconv.ParseUint(rec[2], 10, 64)
		if err != nil {
			port = 0
		}
		h := Host{
			Name:   rec[0],
			Server: server,
			Port:   uint(port),
		}
		ret.Hosts[h.Name] = h
	}
	return ret, nil
}

type Host struct {
	Name   string
	Server string
	Port   uint
}
