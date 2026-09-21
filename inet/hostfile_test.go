package inet

import (
	"reflect"
	"testing"
)

func TestNewHostfile(t *testing.T) {
	type args struct {
		fileName string
		hostName string
	}
	tests := []struct {
		name    string
		args    args
		want    Host
		wantErr bool
	}{
		{"Bad file", args{"../cmd/m17-gateway/packaging/M17Hosts.txtX", ""}, Host{}, true},
		{"M17-M17", args{"../cmd/m17-gateway/packaging/M17Hosts.txt", "M17-M17"}, Host{"M17-M17", "107.191.121.105", 17000}, false},
		{"M17-IP6", args{"../cmd/m17-gateway/packaging/M17Hosts.txt", "M17-IP6"}, Host{"M17-IP6", "[2401:c080:2000:2c78:5400:4ff:fe51:7afe]", 17000}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf, err := NewHostfile(tt.args.fileName)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHostfile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if hf != nil {
				got := hf.Hosts[tt.args.hostName]
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("NewHostfile() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
