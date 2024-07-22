/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package commands

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/sciabarracom/openwhisk-cli/wski18n"
	"github.com/apache/openwhisk-client-go/whisk"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	PollInterval             = time.Second * 2
	Delay                    = time.Second * 5
	MAX_ACTIVATION_LIMIT     = 200
	DEFAULT_ACTIVATION_LIMIT = 30
)

var activationCmd = &cobra.Command{
	Use:   "activation",
	Short: wski18n.T("work with activations"),
}

var activationListCmd = &cobra.Command{
	Use:           "list [NAMESPACE or NAME]",
	Short:         wski18n.T("list activations"),
	SilenceUsage:  true,
	SilenceErrors: true,
	PreRunE:       SetupClientConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		var qualifiedName = new(QualifiedName)
		var orderedFilteredRow []whisk.ActivationFilteredRow

		if whiskErr := CheckArgs(args, 0, 1, "Activation list",
			wski18n.T("An optional namespace is the only valid argument.")); whiskErr != nil {
			return whiskErr
		}

		// Specifying an activation item name filter is optional
		if len(args) == 1 {
			whisk.Debug(whisk.DbgInfo, "Activation item name filter '%s' provided\n", args[0])

			if qualifiedName, err = NewQualifiedName(args[0]); err != nil {
				return NewQualifiedNameError(args[0], err)
			}

			Client.Namespace = qualifiedName.GetNamespace()
		}

		options := &whisk.ActivationListOptions{
			Name:  qualifiedName.GetEntityName(),
			Limit: Flags.common.limit,
			Skip:  Flags.common.skip,
			Upto:  Flags.activation.upto,
			Since: Flags.activation.since,
			Docs:  Flags.common.full,
		}

		activations, _, err := Client.Activations.List(options)
		if err != nil {
			whisk.Debug(whisk.DbgError, "Client.Activations.List() error: %s\n", err)
			errStr := wski18n.T("Unable to obtain the list of activations for namespace '{{.name}}': {{.err}}",
				map[string]interface{}{"name": getClientNamespace(), "err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL,
				whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}

		// When the --full (URL contains "?docs=true") option is specified, display the entire activation details
		if options.Docs == true {
			printFullActivationList(activations)
		} else {
			maxKindSize := max(len("Kind"), getLargestKindSize(activations))
			maxStatusSize := max(len("Status"), getLargestStatusSize(activations))

			// Header string should show "Datetime", "Activation ID", "Kind", "Start", "Duration", "Status", "Entity", with Kind and Status being
			// dynamically sized. The last column Entity will be sized correctly when printed, so no need to calculate size here
			headerFmt := "%-19s %-32s %-" + strconv.Itoa(maxKindSize) + "s %-6s%-10s %-" + strconv.Itoa(maxStatusSize) + "s %-6s\n"
			rowFmt := "%d-%02d-%02d %02d:%02d:%02d %-32s %-" + strconv.Itoa(maxKindSize) + "s %-5s %-10v %-" + strconv.Itoa(maxStatusSize) + "s %-"

			for i := 0; i < len(activations); i++ {
				orderedFilteredRow = append(orderedFilteredRow, whisk.ActivationFilteredRow{Row: activations[i], HeaderFmt: headerFmt, RowFmt: rowFmt})
			}
			printList(orderedFilteredRow, false) // Default sorting for Activations are by creation time, hence sortByName is always false
		}

		return nil
	},
}

var activationGetCmd = &cobra.Command{
	Use:           "get (ACTIVATION_ID | --last) [FIELD_FILTER]",
	Short:         wski18n.T("get activation"),
	SilenceUsage:  true,
	SilenceErrors: true,
	PreRunE:       SetupClientConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		var field string
		var err error

		if args, err = lastFlag(args); err != nil { // Checks if any errors occurred in lastFlag(args)
			whisk.Debug(whisk.DbgError, "lastFlag(%#v) failed: %s\n", args, err)
			errStr := wski18n.T("Unable to get activation: {{.err}}",
				map[string]interface{}{"err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}
		if whiskErr := CheckArgs(args, 1, 2, "Activation get",
			wski18n.T("An activation ID is required.")); whiskErr != nil {
			return whiskErr
		}

		if len(args) > 1 {
			field = args[1]

			if !fieldExists(&whisk.Activation{}, field) {
				errMsg := wski18n.T("Invalid field filter '{{.arg}}'.", map[string]interface{}{"arg": field})
				whiskErr := whisk.MakeWskError(errors.New(errMsg), whisk.EXIT_CODE_ERR_GENERAL,
					whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
				return whiskErr
			}
		}

		id := args[0]
		activation, _, err := Client.Activations.Get(id)
		if err != nil {
			whisk.Debug(whisk.DbgError, "Client.Activations.Get(%s) failed: %s\n", id, err)
			errStr := wski18n.T("Unable to get activation '{{.id}}': {{.err}}",
				map[string]interface{}{"id": id, "err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}

		if Flags.common.summary {
			fmt.Printf(
				wski18n.T("activation result for '/{{.namespace}}/{{.name}}' ({{.status}} at {{.time}})\n",
					map[string]interface{}{
						"namespace": activation.Namespace,
						"name":      activation.Name,
						"status":    activation.Response.Status,
						"time":      time.Unix(activation.End/1000, 0)}))
			printJSON(activation.Response.Result)
		} else if Flags.activation.logs {
			printStrippedActivationLogs(activation.Logs)
		} else {
			if len(field) > 0 {
				fmt.Fprintf(color.Output,
					wski18n.T("{{.ok}} got activation {{.id}}, displaying field {{.field}}\n",
						map[string]interface{}{"ok": color.GreenString("ok:"), "id": boldString(id),
							"field": boldString(field)}))
				printField(activation, field)
			} else {
				fmt.Fprintf(color.Output, wski18n.T("{{.ok}} got activation {{.id}}\n",
					map[string]interface{}{"ok": color.GreenString("ok:"), "id": boldString(id)}))
				printJSON(activation)
			}
		}

		return nil
	},
}

var activationLogsCmd = &cobra.Command{
	Use:           "logs (ACTIVATION_ID | --last)",
	Short:         wski18n.T("get the logs of an activation"),
	SilenceUsage:  true,
	SilenceErrors: true,
	PreRunE:       SetupClientConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error

		if args, err = lastFlag(args); err != nil { // Checks if any errors occurred in lastFlag(args)
			whisk.Debug(whisk.DbgError, "lastFlag(%#v) failed: %s\n", args, err)
			errStr := wski18n.T("Unable to get logs for activation: {{.err}}",
				map[string]interface{}{"err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}
		if whiskErr := CheckArgs(args, 1, 1, "Activation logs",
			wski18n.T("An activation ID is required.")); whiskErr != nil {
			return whiskErr
		}

		id := args[0]
		activation, _, err := Client.Activations.Logs(id)
		if err != nil {
			whisk.Debug(whisk.DbgError, "Client.Activations.Logs(%s) failed: %s\n", id, err)
			errStr := wski18n.T("Unable to get logs for activation '{{.id}}': {{.err}}",
				map[string]interface{}{"id": id, "err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}

		if !Flags.activation.strip {
			printActivationLogs(activation.Logs)
		} else {
			printStrippedActivationLogs(activation.Logs)
		}

		return nil
	},
}

var activationResultCmd = &cobra.Command{
	Use:           "result (ACTIVATION_ID | --last)",
	Short:         "get the result of an activation",
	SilenceUsage:  true,
	SilenceErrors: true,
	PreRunE:       SetupClientConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error

		if args, err = lastFlag(args); err != nil { // Checks if any errors occurred in lastFlag(args)
			whisk.Debug(whisk.DbgError, "lastFlag(%#v) failed: %s\n", args, err)
			errStr := wski18n.T("Unable to get result for activation: {{.err}}",
				map[string]interface{}{"err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}
		if whiskErr := CheckArgs(args, 1, 1, "Activation result",
			wski18n.T("An activation ID is required.")); whiskErr != nil {
			return whiskErr
		}

		id := args[0]
		result, _, err := Client.Activations.Result(id)
		if err != nil {
			whisk.Debug(whisk.DbgError, "Client.Activations.result(%s) failed: %s\n", id, err)
			errStr := wski18n.T("Unable to get result for activation '{{.id}}': {{.err}}",
				map[string]interface{}{"id": id, "err": err})
			werr := whisk.MakeWskErrorFromWskError(errors.New(errStr), err, whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.NO_DISPLAY_USAGE)
			return werr
		}

		printJSON(result.Result)
		return nil
	},
}

// lastFlag(args) retrieves the last activation with flag -l or --last
// Param: Brings in []strings from args
// Return: Returns a []string with the latest ID or the original args and any errors
func lastFlag(args []string) ([]string, error) {
	if Flags.activation.last {
		options := &whisk.ActivationListOptions{
			Limit: 1,
			Skip:  0,
		}
		activations, _, err := Client.Activations.List(options)
		if err != nil { // Checks Activations.List for errors when retrieving latest activation
			whisk.Debug(whisk.DbgError, "Client.Activations.List(%#v) error during lastFlag: %s\n", options, err)
			return args, err
		}
		if len(activations) == 0 { // Checks to see if there are activations available
			whisk.Debug(whisk.DbgError, "No activations found in activation list\n")
			errStr := wski18n.T("Activation list does not contain any activations.")
			whiskErr := whisk.MakeWskError(errors.New(errStr), whisk.EXIT_CODE_ERR_GENERAL, whisk.DISPLAY_MSG, whisk.DISPLAY_USAGE)
			return args, whiskErr
		}
		if len(args) == 0 {
			whisk.Debug(whisk.DbgInfo, "Appending most recent activation ID(%s) into args\n", activations[0].ActivationID)
			args = append(args, activations[0].ActivationID)
		} else {
			whisk.Debug(whisk.DbgInfo, "Appending most recent activation ID(%s) into args\n", activations[0].ActivationID)
			args = append(args, activations[0].ActivationID)
			whisk.Debug(whisk.DbgInfo, "Allocating appended ID to correct position in args\n")
			args[0], args[len(args)-1] = args[len(args)-1], args[0] // IDs should be located at args[0], if 1 or more arguments are given ID has to be moved to args[0]
		}
	}
	return args, nil
}

var activationPollCmd = &cobra.Command{
	Use:           "poll [ NAMESPACE | ACTION_NAME ]",
	Short:         wski18n.T("poll continuously for log messages from currently running actions"),
	SilenceUsage:  true,
	SilenceErrors: true,
	PreRunE:       SetupClientConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		var name string
		var pollSince int64 // Represents an instant in time (in milliseconds since Jan 1 1970)

		if len(args) == 1 {
			name = args[0]
		} else if whiskErr := CheckArgs(args, 0, 1, "Activation poll",
			wski18n.T("An optional namespace is the only valid argument.")); whiskErr != nil {
			return whiskErr
		}

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		signal.Notify(c, syscall.SIGTERM)
		go func() {
			<-c
			fmt.Println(wski18n.T("Poll terminated"))
			os.Exit(1)
		}()
		fmt.Println(wski18n.T("Enter Ctrl-c to exit."))

		// Map used to track activation records already displayed to the console
		reported := make(map[string]bool)

		if Flags.activation.sinceSeconds+
			Flags.activation.sinceMinutes+
			Flags.activation.sinceHours+
			Flags.activation.sinceDays ==
			0 {
			options := &whisk.ActivationListOptions{
				Limit: 1,
				Docs:  true,
			}
			activationList, _, err := Client.Activations.List(options)
			if err != nil {
				whisk.Debug(whisk.DbgWarn, "Client.Activations.List() error: %s\n", err)
				whisk.Debug(whisk.DbgWarn, "Ignoring Client.Activations.List failure; polling for activations since 'now'\n")
				pollSince = time.Now().Unix() * 1000 // Convert to milliseconds
			} else {
				if len(activationList) > 0 {
					lastActivation := activationList[0]  // Activation.Start is in milliseconds since Jan 1 1970
					pollSince = lastActivation.Start + 1 // Use it's start time as the basis of the polling
				}
			}
		} else {
			pollSince = time.Now().Unix() * 1000 // Convert to milliseconds

			// ParseDuration takes a string like "2h45m15s"; create this duration string from the command arguments
			durationStr := fmt.Sprintf("%dh%dm%ds",
				Flags.activation.sinceHours+Flags.activation.sinceDays*24,
				Flags.activation.sinceMinutes,
				Flags.activation.sinceSeconds,
			)
			duration, err := time.ParseDuration(durationStr)
			if err == nil {
				pollSince = pollSince - duration.Nanoseconds()/1000/1000 // Convert to milliseconds
			} else {
				whisk.Debug(whisk.DbgError, "time.ParseDuration(%s) failure: %s\n", durationStr, err)
			}
		}

		fmt.Printf(wski18n.T("Polling for activation logs\n"))
		whisk.Verbose("Polling starts from %s\n", time.Unix(pollSince/1000, 0))
		localStartTime := time.Now()

		// Polling loop
		for {
			if Flags.activation.exit > 0 {
				localDuration := time.Since(localStartTime)
				if int(localDuration.Seconds()) > Flags.activation.exit {
					whisk.Debug(whisk.DbgInfo, "Poll time (%d seconds) expired; polling loop stopped\n", Flags.activation.exit)
					return nil
				}
			}
			whisk.Verbose("Polling for activations since %s\n", time.Unix(pollSince/1000, 0))
			options := &whisk.ActivationListOptions{
				Name:  name,
				Since: pollSince,
				Docs:  true,
				Limit: 0,
				Skip:  0,
			}

			activations, _, err := Client.Activations.List(options)
			if err != nil {
				whisk.Debug(whisk.DbgWarn, "Client.Activations.List() error: %s\n", err)
				whisk.Debug(whisk.DbgWarn, "Ignoring Client.Activations.List failure; continuing to poll for activations\n")
				continue
			}

			for _, activation := range activations {
				if reported[activation.ActivationID] == true {
					continue
				} else {
					fmt.Printf(
						wski18n.T("\nActivation: '{{.name}}' ({{.id}})\n",
							map[string]interface{}{"name": activation.Name, "id": activation.ActivationID}))
					printJSON(activation.Logs)
					reported[activation.ActivationID] = true
				}
				if activation.Start > pollSince {
					pollSince = activation.Start + 1
				}
			}
			time.Sleep(time.Second * 2)
		}
		return nil
	},
}

// Find the size needed for the Kind column when listing activations
func getLargestKindSize(activations []whisk.Activation) int {
	var maxLen = 0
	var curLen int
	var kind interface{}

	for i := 0; i < len(activations); i++ {
		kind = activations[i].Annotations.GetValue("kind")
		if kind == nil {
			kind = "unknown"
		}
		curLen = len(kind.(string))
		if curLen > maxLen {
			maxLen = curLen
		}
	}
	return maxLen
}

// Find the size needed for the Status column when listing activations
func getLargestStatusSize(activations []whisk.Activation) int {
	// The first array in the StatusCodes variable is "success"
	var maxLen = len(whisk.StatusCodes[0])
	var curLen int

	for i := 0; i < len(activations); i++ {
		if activations[i].StatusCode > 0 && activations[i].StatusCode < len(whisk.StatusCodes) {
			curLen = len(whisk.StatusCodes[activations[i].StatusCode])
			if curLen > maxLen {
				maxLen = curLen
			}
		}
	}
	return maxLen
}

func init() {
	activationListCmd.Flags().IntVarP(&Flags.common.skip, "skip", "s", 0, wski18n.T("exclude the first `SKIP` number of activations from the result"))
	activationListCmd.Flags().IntVarP(&Flags.common.limit, "limit", "l", DEFAULT_ACTIVATION_LIMIT, wski18n.T("only return `LIMIT` number of activations from the collection with a maximum LIMIT of {{.max}} activations",
		map[string]interface{}{"max": MAX_ACTIVATION_LIMIT}))
	activationListCmd.Flags().BoolVarP(&Flags.common.full, "full", "f", false, wski18n.T("include full activation description"))
	activationListCmd.Flags().Int64Var(&Flags.activation.upto, "upto", 0, wski18n.T("return activations with timestamps earlier than `UPTO`; measured in milliseconds since Th, 01, Jan 1970"))
	activationListCmd.Flags().Int64Var(&Flags.activation.since, "since", 0, wski18n.T("return activations with timestamps later than `SINCE`; measured in milliseconds since Th, 01, Jan 1970"))

	activationGetCmd.Flags().BoolVarP(&Flags.common.summary, "summary", "s", false, wski18n.T("summarize activation details"))
	activationGetCmd.Flags().BoolVarP(&Flags.activation.last, "last", "l", false, wski18n.T("retrieves the last activation"))
	activationGetCmd.Flags().BoolVarP(&Flags.activation.logs, "logs", "g", false, wski18n.T("emit only the logs, stripped of time stamps and stream identifier"))

	activationLogsCmd.Flags().BoolVarP(&Flags.activation.last, "last", "l", false, wski18n.T("retrieves the last activation"))
	activationLogsCmd.Flags().BoolVarP(&Flags.activation.strip, "strip", "r", false, wski18n.T("strip timestamp and stream information"))
	activationResultCmd.Flags().BoolVarP(&Flags.activation.last, "last", "l", false, wski18n.T("retrieves the last activation"))

	activationPollCmd.Flags().IntVarP(&Flags.activation.exit, "exit", "e", 0, wski18n.T("stop polling after `SECONDS` seconds"))
	activationPollCmd.Flags().IntVar(&Flags.activation.sinceSeconds, "since-seconds", 0, wski18n.T("start polling for activations `SECONDS` seconds ago"))
	activationPollCmd.Flags().IntVar(&Flags.activation.sinceMinutes, "since-minutes", 0, wski18n.T("start polling for activations `MINUTES` minutes ago"))
	activationPollCmd.Flags().IntVar(&Flags.activation.sinceHours, "since-hours", 0, wski18n.T("start polling for activations `HOURS` hours ago"))
	activationPollCmd.Flags().IntVar(&Flags.activation.sinceDays, "since-days", 0, wski18n.T("start polling for activations `DAYS` days ago"))

	activationCmd.AddCommand(
		activationListCmd,
		activationGetCmd,
		activationLogsCmd,
		activationResultCmd,
		activationPollCmd,
	)
}
