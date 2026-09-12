package main

import "testing"

func TestParsePTZStatus(t *testing.T) {
	xml := []byte(`
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tptz:GetStatusResponse>
      <tptz:PTZStatus>
        <tt:Position>
          <tt:PanTilt x="0.42" y="-0.15" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace"/>
          <tt:Zoom x="0.25" space="http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace"/>
        </tt:Position>
      </tptz:PTZStatus>
    </tptz:GetStatusResponse>
  </s:Body>
</s:Envelope>`)
	position, err := parsePTZStatus(xml)
	if err != nil {
		t.Fatal(err)
	}
	if position.Pan != 0.42 || position.Tilt != -0.15 || position.Zoom != 0.25 {
		t.Fatalf("unexpected position: %+v", position)
	}
	if position.PanTiltSpace == "" || position.ZoomSpace == "" {
		t.Fatalf("spaces were not preserved: %+v", position)
	}
}

func TestParsePTZStatusUsesDefaultSpaces(t *testing.T) {
	xml := []byte(`<Envelope><Body><GetStatusResponse><PTZStatus><Position><PanTilt x="1" y="0"/></Position></PTZStatus></GetStatusResponse></Body></Envelope>`)
	position, err := parsePTZStatus(xml)
	if err != nil {
		t.Fatal(err)
	}
	if position.Pan != 1 || position.Tilt != 0 {
		t.Fatalf("unexpected position: %+v", position)
	}
	if position.PanTiltSpace != defaultPanTiltSpace || position.ZoomSpace != defaultZoomSpace {
		t.Fatalf("default spaces were not applied: %+v", position)
	}
}

func TestUpsertAndRemovePTZFavorite(t *testing.T) {
	list := upsertPTZFavorite(nil, ptzFavorite{Name: "Portao", Pan: 0.1})
	list = upsertPTZFavorite(list, ptzFavorite{Name: "portao", Pan: 0.5, Tilt: 0.2})
	if len(list) != 1 || list[0].Pan != 0.5 {
		t.Fatalf("upsert should replace by name: %+v", list)
	}
	list = upsertPTZFavorite(list, ptzFavorite{Name: "Garagem", Pan: -0.3})
	list = removePTZFavorite(list, "Portao")
	if len(list) != 1 || list[0].Name != "Garagem" {
		t.Fatalf("remove left unexpected list: %+v", list)
	}
}

func TestParsePresetToken(t *testing.T) {
	xml := []byte(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body><SetPresetResponse><PresetToken>3</PresetToken></SetPresetResponse></s:Body></s:Envelope>`)
	if got := parsePresetToken(xml); got != "3" {
		t.Fatalf("preset token = %q", got)
	}
}

func TestSOAPFaultText(t *testing.T) {
	xml := []byte(`<env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><env:Fault><env:Code><env:Value>env:Receiver</env:Value></env:Code><env:Reason><env:Text>Action Not Implemented</env:Text></env:Reason></env:Fault></env:Body></env:Envelope>`)
	if got := soapFaultText(xml); got != "Action Not Implemented" {
		t.Fatalf("fault text = %q", got)
	}
}
