package workflow

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/fatih/structs"
	"github.com/go-gorp/gorp"

	"github.com/ovh/cds/engine/api/cache"
	"github.com/ovh/cds/engine/api/event"
	"github.com/ovh/cds/engine/api/repositoriesmanager"
	"github.com/ovh/cds/engine/api/tracing"
	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/log"
)

// GetWorkflowRunEventData read channel to get elements to push
// TODO: refactor this useless function
func GetWorkflowRunEventData(report *ProcessorReport, projectKey string) ([]sdk.WorkflowRun, []sdk.WorkflowNodeRun, []sdk.WorkflowNodeJobRun) {
	return report.workflows, report.nodes, report.jobs
}

// SendEvent Send event on workflow run
func SendEvent(db gorp.SqlExecutor, wrs []sdk.WorkflowRun, wnrs []sdk.WorkflowNodeRun, wnjrs []sdk.WorkflowNodeJobRun, key string) {
	for _, wr := range wrs {
		event.PublishWorkflowRun(wr, key)
	}
	for _, wnr := range wnrs {
		wr, errWR := LoadRunByID(db, wnr.WorkflowRunID, LoadRunOptions{
			WithLightTests: true,
		})
		if errWR != nil {
			log.Warning("SendEvent.workflow> Cannot load workflow run %d: %s", wnr.WorkflowRunID, errWR)
			continue
		}

		var previousNodeRun sdk.WorkflowNodeRun
		if wnr.SubNumber > 0 {
			previousNodeRun = wnr
		} else {
			// Load previous run on current node
			node := wr.Workflow.GetNode(wnr.WorkflowNodeID)
			if node != nil {
				var errN error
				previousNodeRun, errN = PreviousNodeRun(db, wnr, *node, wr.WorkflowID)
				if errN != nil {
					log.Debug("SendEvent.workflow> Cannot load previous node run: %s", errN)
				}
			} else {
				log.Warning("SendEvent.workflow > Unable to find node %d in workflow", wnr.WorkflowNodeID)
			}
		}

		event.PublishWorkflowNodeRun(db, wnr, *wr, &previousNodeRun, key)
	}
	for _, wnjr := range wnjrs {
		wnr, errWNR := LoadNodeRunByID(db, wnjr.WorkflowNodeRunID, LoadRunOptions{
			WithLightTests: true,
		})
		if errWNR != nil {
			log.Warning("SendEvent.workflow.wnjrs > Unable to find workflow node run %d: %s", wnjr.WorkflowNodeRunID, errWNR)
			continue
		}

		wr, errWR := LoadRunByID(db, wnr.WorkflowRunID, LoadRunOptions{
			WithLightTests: true,
		})
		if errWR != nil {
			log.Warning("SendEvent.workflow.wnjrs> Unable to load workflow run %d: %s", wnr.WorkflowRunID, errWR)
			continue
		}
		event.PublishWorkflowNodeRun(db, *wnr, *wr, nil, key)
		event.PublishWorkflowNodeJobRun(key, wnjr, *wnr, *wr)
	}
}

func resyncCommitStatus(ctx context.Context, db gorp.SqlExecutor, store cache.Store, proj *sdk.Project, wr *sdk.WorkflowRun) error {
	_, end := tracing.Span(ctx, "workflow.resyncCommitStatus",
		tracing.Tag("workflow", wr.Workflow.Name),
		tracing.Tag("workflow_run", wr.Number),
	)
	defer end()

	for nodeID, nodeRuns := range wr.WorkflowNodeRuns {
		sort.Slice(nodeRuns, func(i, j int) bool {
			return nodeRuns[i].SubNumber >= nodeRuns[j].SubNumber
		})

		nodeRun := nodeRuns[0]
		if !sdk.StatusIsTerminated(nodeRun.Status) {
			continue
		}

		node := wr.Workflow.GetNode(nodeID)
		if node.IsLinkedToRepo() {
			vcsServer := repositoriesmanager.GetProjectVCSServer(proj, node.Context.Application.VCSServer)
			if vcsServer == nil {
				return nil
			}

			//Get the RepositoriesManager Client
			client, errClient := repositoriesmanager.AuthorizedClient(db, store, vcsServer)
			if errClient != nil {
				return sdk.WrapError(errClient, "resyncCommitStatus> Cannot get client")
			}

			statuses, errStatuses := client.ListStatuses(node.Context.Application.RepositoryFullname, nodeRun.VCSHash)
			if errStatuses != nil {
				return sdk.WrapError(errStatuses, "resyncCommitStatus> Cannot get statuses")
			}

			var statusFound *sdk.VCSCommitStatus
			expected := sdk.VCSCommitStatusDescription(proj.Key, wr.Workflow.Name, sdk.EventRunWorkflowNode{
				NodeName: node.Name,
			})

			var sendEvent = func() error {
				log.Debug("Resync status for node run %d", nodeRun.ID)
				var eventWNR = sdk.EventRunWorkflowNode{
					ID:             nodeRun.ID,
					Number:         nodeRun.Number,
					SubNumber:      nodeRun.SubNumber,
					Status:         nodeRun.Status,
					Start:          nodeRun.Start.Unix(),
					Done:           nodeRun.Done.Unix(),
					Manual:         nodeRun.Manual,
					HookEvent:      nodeRun.HookEvent,
					Payload:        nodeRun.Payload,
					SourceNodeRuns: nodeRun.SourceNodeRuns,
					Hash:           nodeRun.VCSHash,
					BranchName:     nodeRun.VCSBranch,
					NodeID:         nodeRun.WorkflowNodeID,
					RunID:          nodeRun.WorkflowRunID,
					StagesSummary:  make([]sdk.StageSummary, len(nodeRun.Stages)),
				}

				for i := range nodeRun.Stages {
					eventWNR.StagesSummary[i] = nodeRun.Stages[i].ToSummary()
				}

				var pipName, appName, envName string
				node := wr.Workflow.GetNode(nodeRun.WorkflowNodeID)
				if node != nil {
					pipName = node.Pipeline.Name
					eventWNR.NodeName = node.Name
				}
				if node.Context != nil {
					if node.Context.Application != nil {
						appName = node.Context.Application.Name
						eventWNR.RepositoryManagerName = node.Context.Application.VCSServer
						eventWNR.RepositoryFullName = node.Context.Application.RepositoryFullname
					}
					if node.Context.Environment != nil {
						envName = node.Context.Environment.Name
					}
				}

				evt := sdk.Event{
					EventType:       fmt.Sprintf("%T", eventWNR),
					Payload:         structs.Map(eventWNR),
					Timestamp:       time.Now(),
					ProjectKey:      proj.Key,
					WorkflowName:    wr.Workflow.Name,
					PipelineName:    pipName,
					ApplicationName: appName,
					EnvironmentName: envName,
				}
				if err := client.SetStatus(evt); err != nil {
					repositoriesmanager.RetryEvent(&evt, err, store)
					return fmt.Errorf("resyncCommitStatus> err:%s", err)
				}
				return nil
			}

			for i, status := range statuses {
				if status.Decription == expected {
					statusFound = &statuses[i]
					break
				}
			}

			if statusFound == nil {
				if err := sendEvent(); err != nil {
					log.Error("resyncCommitStatus> Error sending status: %v", err)
				}
				continue
			}

			if statusFound.State == sdk.StatusBuilding.String() {
				if err := sendEvent(); err != nil {
					log.Error("resyncCommitStatus> Error sending status: %v", err)
				}
				continue
			}

			switch statusFound.State {
			case sdk.StatusSuccess.String():
				switch nodeRun.Status {
				case sdk.StatusSuccess.String():
					continue
				default:
					if err := sendEvent(); err != nil {
						log.Error("resyncCommitStatus> Error sending status: %v", err)
					}
					continue
				}

			case sdk.StatusFail.String():
				switch nodeRun.Status {
				case sdk.StatusFail.String():
					continue
				default:
					if err := sendEvent(); err != nil {
						log.Error("resyncCommitStatus> Error sending status: %v", err)
					}
					continue
				}

			case sdk.StatusSkipped.String():
				switch nodeRun.Status {
				case sdk.StatusDisabled.String(), sdk.StatusNeverBuilt.String(), sdk.StatusSkipped.String():
					continue
				default:
					if err := sendEvent(); err != nil {
						log.Error("resyncCommitStatus> Error sending status: %v", err)
					}
					continue
				}
			}
		}
	}
	return nil
}
