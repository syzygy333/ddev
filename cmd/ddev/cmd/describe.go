package cmd

import (
	"bytes"
	"fmt"
	"github.com/drud/ddev/pkg/ddevapp"
	"github.com/drud/ddev/pkg/globalconfig"
	"github.com/drud/ddev/pkg/nodeps"
	"github.com/drud/ddev/pkg/output"
	"github.com/drud/ddev/pkg/styles"
	"github.com/drud/ddev/pkg/util"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
)

// DescribeCommand represents the `ddev config` command
var DescribeCommand = &cobra.Command{
	Use:     "describe [projectname]",
	Aliases: []string{"status", "st", "desc"},
	Short:   "Get a detailed description of a running ddev project.",
	Long: `Get a detailed description of a running ddev project. Describe provides basic
information about a ddev project, including its name, location, url, and status.
It also provides details for MySQL connections, and connection information for
additional services like MailHog and phpMyAdmin. You can run 'ddev describe' from
a project directory to describe that project, or you can specify a project to describe by
running 'ddev describe <projectname>.`,
	Example: "ddev describe\nddev describe <projectname>\nddev status\nddev st",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 1 {
			util.Failed("Too many arguments provided. Please use 'ddev describe' or 'ddev describe [projectname]'")
		}

		apps, err := getRequestedProjects(args, false)
		if err != nil {
			util.Failed("Failed to describe project(s): %v", err)
		}
		app := apps[0]

		if err := ddevapp.CheckForMissingProjectFiles(app); err != nil {
			util.Failed("Failed to describe %s: %v", app.Name, err)
		}

		desc, err := app.Describe(false)
		if err != nil {
			util.Failed("Failed to describe project %s: %v", app.Name, err)
		}

		renderedDesc, err := renderAppDescribe(app, desc)
		util.CheckErr(err) // We shouldn't ever end up with an unrenderable desc.
		output.UserOut.WithField("raw", desc).Print(renderedDesc)
	},
}

// renderAppDescribe takes the map describing the app and renders it for plain-text output
func renderAppDescribe(app *ddevapp.DdevApp, desc map[string]interface{}) (string, error) {
	status := desc["status"]
	var out bytes.Buffer

	t := table.NewWriter()
	t.SetOutputMirror(&out)
	styles.SetGlobalTableStyle(t)
	tWidth, _ := nodeps.GetTerminalWidthHeight()
	urlPortWidth := float64(35)
	infoWidth := 30
	urlPortWidthFactor := float64(2.5)
	if tWidth != 0 {
		urlPortWidth = float64(tWidth) / urlPortWidthFactor
		infoWidth = tWidth / 4
	}
	output.UserOut.Debugf("tWidth=%v urlPortWidth=%v infoWidth=%v", tWidth, urlPortWidth, infoWidth)
	t.SetAllowedRowLength(tWidth)
	t.SetColumnConfigs([]table.ColumnConfig{
		{
			Name:     "Service",
			WidthMax: 8,
		},
		{
			Name:     "URL/Port",
			WidthMax: int(urlPortWidth),
		},
		{
			Name:     "Info",
			WidthMax: infoWidth,
		},
	})
	t.AppendHeader(table.Row{"Service", "Stat", "URL/Port", "Info"})
	t.AppendRow(table.Row{"Project", "", app.Name, desc["shortroot"].(string)})

	// Only show extended status for running sites.
	if status == ddevapp.SiteRunning {
		serviceNames := []string{}
		// Get a list of services in the order we want them, with web and db first
		serviceMap := desc["services"].(map[string]map[string]string)
		for k := range serviceMap {
			if k != "web" && k != "db" {
				serviceNames = append(serviceNames, k)
			}
		}
		sort.Strings(serviceNames)

		if _, ok := desc["dbinfo"]; ok {
			serviceNames = append([]string{"db"}, serviceNames...)
		}
		serviceNames = append([]string{"web"}, serviceNames...)

		for _, k := range serviceNames {
			v := serviceMap[k]

			httpURL := ""
			urlPortParts := []string{}
			if !ddevapp.IsRouterDisabled(app) {
				if httpsURL, ok := v["https_url"]; ok {
					urlPortParts = append(urlPortParts, httpsURL)
				} else if httpURL, ok = v["http_url"]; ok {
					urlPortParts = append(urlPortParts, httpURL)
				}
			} else {
				httpURL = v["host_http_url"]
				if httpURL != "" {
					urlPortParts = append(urlPortParts, httpURL)
				}
			}
			if p, ok := v["exposed_ports"]; ok {
				urlPortParts = append(urlPortParts, "InDocker: "+v["full_name"]+":"+p)
			}
			if p, ok := v["host_ports"]; ok && p != "" {
				urlPortParts = append(urlPortParts, "Host: localhost:"+p)
			}

			extraInfo := []string{}

			// Get extra info for web container
			if k == "web" {
				extraInfo = append(extraInfo, fmt.Sprintf("PHP %s %s", desc["php_version"], desc["webserver_type"]))
				if desc["nfs_mount_enabled"].(bool) {
					extraInfo = append(extraInfo, fmt.Sprintf("NFS Enabled"))
				}
				if desc["mutagen_enabled"].(bool) {
					extraInfo = append(extraInfo, fmt.Sprintf("Mutagen enabled (%s)", ddevapp.FormatSiteStatus(desc["mutagen_status"].(string))))
				}
			}

			// Get extra info for db container
			if k == "db" {
				if _, ok := desc["dbinfo"]; ok {
					dbinfo := desc["dbinfo"].(map[string]interface{})
					if _, ok := dbinfo["mariadb_version"]; ok {
						extraInfo = append(extraInfo, "MariaDB "+dbinfo["mariadb_version"].(string))
					} else if _, ok := dbinfo["mysql_version"]; ok {
						extraInfo = append(extraInfo, "MySQL"+dbinfo["mysql_version"].(string))
					} else {
						extraInfo = append(extraInfo, dbinfo["database_type"].(string))
					}
				}
				extraInfo = append(extraInfo, "User/Pass: 'db/db'\nor 'root/root'")
			}
			if k == "dba" {
				k = "PHPMyAdmin"
				urlPortParts = append(urlPortParts, "`ddev launch -p`")
			}
			t.AppendRow(table.Row{k, v["status"], strings.Join(urlPortParts, "\n"), strings.Join(extraInfo, "\n")})
		}

		if !ddevapp.IsRouterDisabled(app) {
			mailhogURL := ""
			if _, ok := desc["mailhog_url"]; ok {
				mailhogURL = desc["mailhog_url"].(string)
			}
			if _, ok := desc["mailhog_https_url"]; ok {
				mailhogURL = desc["mailhog_https_url"].(string)
			}

			t.AppendRow(table.Row{"Mailhog", "", fmt.Sprintf("MailHog: %s\n`ddev launch -m`", mailhogURL)})
			_, _, urls := app.GetAllURLs()
			s := strings.Join(urls, ", ")
			urlString := text.WrapSoft(s, int(urlPortWidth))
			t.AppendRow(table.Row{"All URLs", "", urlString})
		}
		bindInfo := []string{}
		if app.BindAllInterfaces {
			bindInfo = append(bindInfo, "bind-all-interfaces ENABLED")
		}
		if globalconfig.DdevGlobalConfig.RouterBindAllInterfaces && !ddevapp.IsRouterDisabled(app) {
			bindInfo = append(bindInfo, "router-bind-all-interfaces ENABLED")
		}
		if len(bindInfo) > 0 {
			t.AppendRow(table.Row{"Network", "", strings.Join(bindInfo, "\n")})
		}
	}

	t.Render()

	return out.String(), nil
}

func init() {
	RootCmd.AddCommand(DescribeCommand)
}
