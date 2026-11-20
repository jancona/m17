package mqttstatus

// The objects published on <Name>/json. Field names and meanings follow
// M17-Project/MMDVMHost's schema.json (MMDVM, Modem, M17, M17Packet) and
// G4KLX's gateway programs' (status, link). M17GNSS, the can fields, and
// link's module and its "rejected" and "shutdown" reasons are additions
// proposed to those schemas (docs/mqtt-design.md).

// MMDVM is a mode change ({"mode": "idle"}) or a lifecycle message
// ({"message": "m17-gateway is starting"}).
type MMDVM struct {
	Timestamp string `json:"timestamp"`
	Mode      string `json:"mode,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Status is the gateway programs' lifecycle message.
type Status struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// Modem reports the modem opening or closing.
type Modem struct {
	Timestamp   string   `json:"timestamp"`
	Open        bool     `json:"open"`
	Protocol    int      `json:"protocol,omitempty"` // MMDVM protocol version
	Description string   `json:"description,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Connection  string   `json:"connection,omitempty"` // uart or spi
	Port        string   `json:"port,omitempty"`
	Speed       int      `json:"speed,omitempty"`
}

// M17 is a voice stream event. The start carries the callsigns and
// traffic type; the end its duration and bit error rate.
type M17 struct {
	Timestamp   string   `json:"timestamp"`
	SrcCallsign string   `json:"src_callsign,omitempty"`
	DstCallsign string   `json:"dst_callsign,omitempty"`
	Source      string   `json:"source"` // rf or network
	Action      string   `json:"action"` // start, late_entry, end, lost
	TrafficType string   `json:"traffic_type,omitempty"`
	CAN         *int     `json:"can,omitempty"`
	Duration    *float64 `json:"duration,omitempty"` // seconds
	BER         *float64 `json:"ber,omitempty"`      // percent, before error correction
}

// M17Packet is a packet: SMS text, or other data in hex.
type M17Packet struct {
	Timestamp   string  `json:"timestamp"`
	Source      string  `json:"source"`
	SrcCallsign string  `json:"src_callsign"`
	DstCallsign string  `json:"dst_callsign"`
	Protocol    int     `json:"protocol"`
	Length      int     `json:"length"`
	Text        *string `json:"text,omitempty"`
	Data        string  `json:"data,omitempty"`
	CAN         *int    `json:"can,omitempty"`
}

// M17GNSS is a position from the LSF META field.
type M17GNSS struct {
	Timestamp   string   `json:"timestamp"`
	Source      string   `json:"source"`
	SrcCallsign string   `json:"src_callsign"`
	DataSource  int      `json:"data_source"`
	StationType int      `json:"station_type"`
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	Altitude    *float64 `json:"altitude,omitempty"`
	Speed       *float64 `json:"speed,omitempty"`
	Bearing     *int     `json:"bearing,omitempty"`
	Radius      *int     `json:"radius,omitempty"`
}

// Link is the reflector link state, published retained.
type Link struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"` // linking, unlinked, relinking, failed
	Reason    string `json:"reason,omitempty"`
	Reflector string `json:"reflector,omitempty"`
	Module    string `json:"module,omitempty"`
	Protocol  string `json:"protocol"`
}
