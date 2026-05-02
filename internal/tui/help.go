package tui

import (
	"strings"

	"github.com/tesserix/cloudnav/internal/tui/styles"
)

func (m *model) helpView() string {
	body := strings.Join([]string{
		styles.Title.Render("cloudnav keybindings"),
		styles.Header.Render("Nav") + "    ↵/l drill   esc/h back   jk move   / filter   : palette   f flag   r refresh",
		styles.Header.Render("View") + "   i info   o portal   c costs   s sort   t tenant",
		styles.Header.Render("Auth") + "   I login (runs az/gcloud/aws login inside the TUI)",
		styles.Header.Render("Select") + " ␣ toggle   [ select-all   ] clear   D delete   L lock",
		styles.Header.Render("Filter") + " 0-5 on resource views — 0 all / 1 compute / 2 data / 3 network / 4 security / 5 other",
		styles.Header.Render("Ops") + "    A advisor (Azure / GCP / AWS via Compute Optimizer + TA) — cost / security / reliability / perf / ops",
		styles.Header.Render("Health") + " H service-health overlay — active incidents / maintenance / advisories",
		styles.Header.Render("Metrics") + " M metrics on a resource — last 60 min sparklines (Azure Monitor, CloudWatch, GCP Monitoring)",
		styles.Header.Render("Billing") + " B portfolio view — per-sub (Azure), per-service (AWS), per-project (GCP)",
		styles.Header.Render("Costs") + "   $ cost-history chart — last 3 / 6 months with MoM deltas (Azure)",
		styles.Header.Render("Upgrade") + " U upgrade cloudnav when a newer release is available on GitHub",
		styles.Header.Render("PIM") + "    p open — Azure / Entra / Groups / GCP PAM   0/1/2/3/4 filter source",
		"         / filter   a activate   +/- duration   j/k move",
		styles.Header.Render("Term") + "   x open embedded terminal (themed per active cloud — ctrl-q back, ctrl-d/exit close)",
		styles.Header.Render("Theme") + "  : palette   type 'theme' to filter — default / dracula / nord / solarized-dark / solarized-light / monochrome",
		styles.Header.Render("Spinner") + " : palette   type 'spinner' to filter — dot / line / globe / moon / pulse / etc.",
		styles.Header.Render("Misc") + "   ? help   q quit",
		"",
		styles.ModalHint.Render("press any key to close"),
	}, "\n")
	return m.overlay(body)
}
