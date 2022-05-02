/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/ava-labs/avalanche-cli/cmd/prompts"
	"github.com/ava-labs/avalanche-cli/pkg/models"
	"github.com/ava-labs/avalanche-cli/pkg/vm"
	"github.com/spf13/cobra"
)

var filename string

// var fast bool
var useSubnetEvm *bool
var useSpaces *bool
var useBlob *bool
var useTimestamp *bool
var useCustom *bool

const subnetEvm = "SubnetEVM"
const spacesVm = "Spaces VM"
const blobVm = "Blob VM"
const timestampVm = "Timestamp VM"
const customVm = "Custom"

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create [subnetName]",
	Short: "Create a new subnet genesis",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Args: cobra.ExactArgs(1),
	Run:  createGenesis,
}

func init() {
	subnetCmd.AddCommand(createCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// createCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	createCmd.Flags().StringVarP(&filename, "filename", "f", "", "filepath of genesis to use")
	// createCmd.Flags().BoolVarP(&fast, "fast", "z", false, "use default values to minimize configuration")
	useSubnetEvm = createCmd.Flags().Bool("evm", false, "use the SubnetEVM as your VM")
	useSpaces = createCmd.Flags().Bool("spaces", false, "use the Spaces VM as your VM")
	useBlob = createCmd.Flags().Bool("blob", false, "use the Blob VM as your VM")
	useTimestamp = createCmd.Flags().Bool("timestamp", false, "use the Timestamp VM as your VM")
	useCustom = createCmd.Flags().Bool("custom", false, "use your own custom VM as your VM")
}

const BaseDir = ".avalanche-cli"

func writeGenesisFile(subnetName string, genesisBytes []byte) error {
	usr, _ := user.Current()
	genesisPath := filepath.Join(usr.HomeDir, BaseDir, subnetName+genesis_suffix)
	err := os.WriteFile(genesisPath, genesisBytes, 0644)
	return err
}

func copyGenesisFile(inputFilename string, subnetName string) error {
	genesisBytes, err := os.ReadFile(inputFilename)
	if err != nil {
		return err
	}
	usr, _ := user.Current()
	genesisPath := filepath.Join(usr.HomeDir, BaseDir, subnetName+genesis_suffix)
	err = os.WriteFile(genesisPath, genesisBytes, 0644)
	return err
}

const sidecar_suffix = "_sidecar.json"

func createSidecar(subnetName string, vm models.VmType) error {
	sc := models.Sidecar{
		Name:   subnetName,
		Vm:     vm,
		Subnet: subnetName,
	}

	scBytes, err := json.MarshalIndent(sc, "", "    ")
	if err != nil {
		return nil
	}

	usr, _ := user.Current()
	sidecarPath := filepath.Join(usr.HomeDir, BaseDir, subnetName+sidecar_suffix)
	err = os.WriteFile(sidecarPath, scBytes, 0644)
	return err
}

func moreThanOneVmSelected() bool {
	vmVars := []bool{*useSubnetEvm, *useSpaces, *useBlob, *useTimestamp, *useCustom}
	firstSelect := false
	for _, val := range vmVars {
		if firstSelect && val {
			return true
		} else if val {
			firstSelect = true
		}
	}
	return false
}

func getVmFromFlag() models.VmType {
	if *useSubnetEvm {
		return models.SubnetEvm
	}
	if *useSpaces {
		return models.SpacesVm
	}
	if *useBlob {
		return models.BlobVm
	}
	if *useTimestamp {
		return models.TimestampVm
	}
	if *useCustom {
		return models.CustomVm
	}
	return ""
}

func createGenesis(cmd *cobra.Command, args []string) {
	if moreThanOneVmSelected() {
		fmt.Println("Too many VMs selected. Provide at most one VM selection flag.")
		return
	}

	if filename == "" {

		var subnetType models.VmType
		var err error
		subnetType = getVmFromFlag()

		if subnetType == "" {

			subnetTypeStr, err := prompts.CaptureList(
				"Choose your VM",
				[]string{subnetEvm, spacesVm, blobVm, timestampVm, customVm},
			)
			if err != nil {
				fmt.Println(err)
				return
			}
			subnetType = models.VmTypeFromString(subnetTypeStr)
		}

		var genesisBytes []byte

		switch subnetType {
		case subnetEvm:
			genesisBytes, err = vm.CreateEvmGenesis(args[0])
			if err != nil {
				fmt.Println(err)
				return
			}

			err = createSidecar(args[0], models.SubnetEvm)
			if err != nil {
				fmt.Println(err)
				return
			}
		case customVm:
			genesisBytes, err = vm.CreateCustomGenesis(args[0])
			if err != nil {
				fmt.Println(err)
				return
			}
			err = createSidecar(args[0], models.CustomVm)
			if err != nil {
				fmt.Println(err)
				return
			}
		default:
			fmt.Println("Not implemented")
			return
		}

		err = writeGenesisFile(args[0], genesisBytes)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Successfully created genesis")
	} else {
		fmt.Println("Using specified genesis")
		err := copyGenesisFile(filename, args[0])
		if err != nil {
			fmt.Println(err)
			return
		}

		var subnetType models.VmType
		subnetType = getVmFromFlag()

		if subnetType == "" {
			subnetTypeStr, err := prompts.CaptureList(
				"What VM does your genesis use?",
				[]string{subnetEvm, spacesVm, blobVm, timestampVm, customVm},
			)
			if err != nil {
				fmt.Println(err)
				return
			}
			subnetType = models.VmTypeFromString(subnetTypeStr)
		}
		err = createSidecar(args[0], subnetType)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Successfully created genesis")
	}
}
