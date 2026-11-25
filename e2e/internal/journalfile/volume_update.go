package journalfile

import (
	"fmt"

	"github.com/google/shlex"
	"github.com/spf13/pflag"
)

// VolumeUpdateRequest is the supported spelling of an authored same-Team
// Volume configuration update. The Team selector is the request scope, not a
// transfer destination, and at least one storage configuration is required.
type VolumeUpdateRequest struct {
	ID, TeamID, Region, Profile, Output                         string
	TeamSet, RegionSet, ProfileSet, OutputSet, AgentEnvelopeSet bool
	AgentEnvelope                                               bool
	Configurations                                              int
}

func ParseVolumeUpdateRequest(command string) (VolumeUpdateRequest, error) {
	args, err := shlex.Split(command)
	if err != nil || len(args) < 5 || args[0] != "ecctl" {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update grammar")
	}
	flags := pflag.NewFlagSet("owned Volume update", pflag.ContinueOnError)
	for _, name := range []string{"region", "profile", "team-id", "lang", "output", "oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config", "mount-config"} {
		flags.String(name, "", "")
	}
	for _, name := range []string{"json", "agent-envelope", "no-color"} {
		flags.Bool(name, false, "")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update flags: %w", err)
	}
	pos := flags.Args()
	if len(pos) != 4 || (pos[0] != "sandbox" && pos[0] != "sbx") || pos[1] != "volume" || pos[2] != "update" || !safeTemplateID(pos[3]) {
		return VolumeUpdateRequest{}, fmt.Errorf("invalid Volume update target")
	}
	team, _ := flags.GetString("team-id")
	if !flags.Lookup("team-id").Changed || !safeTemplateID(team) {
		return VolumeUpdateRequest{}, fmt.Errorf("Volume update requires the owned Team scope")
	}
	configurations := 0
	for _, name := range []string{"oss-volume-config", "agentic-fs-volume-config", "agentic-bucket-volume-config", "mount-config"} {
		if flags.Lookup(name).Changed {
			configurations++
		}
	}
	if configurations == 0 {
		return VolumeUpdateRequest{}, fmt.Errorf("Volume update requires a storage configuration")
	}
	region, _ := flags.GetString("region")
	profile, _ := flags.GetString("profile")
	output, _ := flags.GetString("output")
	agentEnvelope, _ := flags.GetBool("agent-envelope")
	return VolumeUpdateRequest{ID: pos[3], TeamID: team, TeamSet: true, Region: region, Profile: profile, Output: output, RegionSet: flags.Lookup("region").Changed, ProfileSet: flags.Lookup("profile").Changed, OutputSet: flags.Lookup("output").Changed, AgentEnvelopeSet: flags.Lookup("agent-envelope").Changed, AgentEnvelope: agentEnvelope, Configurations: configurations}, nil
}
