package webuntis

import "testing"

// Synthetic markup modeled after klassengeld.app's dashboard: header,
// balance box, saldo button and a DevExtreme grid with an inline data array.
const kgDashboard = `
<div style="margin-top:20px"><strong style="color: var(--connie-formcontrol-color)">Kid Alpha (6c)</strong> - Gesamtschule Huellhorst </div>
<div style="xwidth:250px">
  <div style="border:1px solid #EEEEEE; padding:10px; margin-top:10px">
    <div style="display:inline-block; float:right">
      <span style="color: #219653">1.160,00 &euro;</span>
    </div>
    <span>Kontostand</span>
  </div>
</div>
<button class="btn" onclick="show_saldo_info(1827739)">Alle Transaktionen anzeigen</button>
<script>
  var dsAbc123 = [{"field0":"Kennenlernfahrt","fieldtxt0":"Kennenlernfahrt Kirsche","field1":"x","fieldtxt1":"{20250922}22.09.2025","fieldtxt2":"160,00 €","fieldtxt3":"<span>bezahlt <i class='fa fa-check'></i></span>","fieldtxt4":"","fieldtxt5":"","fieldtxt6":""},
                  {"fieldtxt0":"Kunstgeld 6c","fieldtxt1":"{20260909}09.09.2026","fieldtxt2":"3,00 €","fieldtxt3":"keine Bezahlung erforderlich","fieldtxt4":"","fieldtxt5":"Info [x]","fieldtxt6":""}];
  $("#grid").dxDataGrid({
    dataSource: dsAbc123,
    columns: [
      { caption: "Projekt", dataField: "fieldtxt0" },
      { caption: "Frist", dataField: "fieldtxt1" },
      { caption: "Betrag", dataField: "fieldtxt2" },
      { caption: "Zahlungsanforderung", dataField: "fieldtxt3" },
      { caption: "", dataField: "fieldtxt4" },
      { caption: "Weitere Informationen", dataField: "fieldtxt5" },
      { caption: "Extras", dataField: "fieldtxt6" },
    ]
  });
</script>`

const kgSaldo = `<h4>Schülerkonto Grieser, Liana</h4>
<table><tr><td>Kennenlernfahrt Kirsche</td><td>160,00 €</td></tr><tr><td>nicht zugeordnete Summe</td><td>0,00 €</td></tr></table>
<script>
var txXyz = [{"field0":"<i class='fa fa-arrow-right' style='color:#4CAF50'></i>","fieldtxt0":"","field1":"03.09.2025","fieldtxt1":"03.09.2025","field2":"160.00","fieldtxt2":"160.00","fieldtxt3":"Banküberweisung","fieldtxt4":"Parent &amp; Co","fieldtxt5":"","fieldtxt6":"","rowkeyHS":"a"}];
$("#tx").dxDataGrid({ dataSource: txXyz, columns: [ { caption: "", dataField: "fieldtxt0" } ] });
</script>`

func TestParseKlassengeldDashboard(t *testing.T) {
	st, err := parseKGDashboard(kgDashboard)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 {
		t.Fatalf("students: %+v", st)
	}
	s := st[0]
	if s.Name != "Kid Alpha (6c)" || s.School != "Gesamtschule Huellhorst" || s.Balance != 1160 || s.AccountID != 1827739 {
		t.Fatalf("student: %+v", s)
	}
	if len(s.Projects) != 2 {
		t.Fatalf("projects: %+v", s.Projects)
	}
	p := s.Projects[0]
	if p.Name != "Kennenlernfahrt Kirsche" || p.Due != "22.09.2025" || p.Amount != 160 || p.Status != "bezahlt" {
		t.Fatalf("project: %+v", p)
	}
	if s.Projects[1].Info != "Info [x]" {
		t.Fatalf("info: %+v", s.Projects[1])
	}
}

func TestParseKlassengeldSaldo(t *testing.T) {
	res, tx := parseKGSaldo(kgSaldo)
	if len(res) != 2 || res[0].Amount != 160 {
		t.Fatalf("reservations: %+v", res)
	}
	if len(tx) != 1 || tx[0].Direction != "in" || tx[0].Amount != 160 || tx[0].Details != "Parent & Co" || tx[0].Type != "Banküberweisung" {
		t.Fatalf("transactions: %+v", tx)
	}
}

func TestParseEuro(t *testing.T) {
	for in, want := range map[string]float64{"160,00 €": 160, "1.234,56": 1234.56, "160.00": 160, "-3,50 &euro;": -3.5} {
		if got := parseEuro(in); got != want {
			t.Errorf("parseEuro(%q) = %v, want %v", in, got, want)
		}
	}
}
