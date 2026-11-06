import { Annotation, END, START, StateGraph } from "@langchain/langgraph";

export const stages = [
  "ingest",
  "align",
  "analyze-assets",
  "analyze-style",
  "plan",
  "render",
] as const;

export type Stage = (typeof stages)[number];

export interface WorkflowContext {
  hasReferences: boolean;
}

export interface GoTools {
  readWorkflowContext(): Promise<WorkflowContext>;
  runStage(stage: Stage, args: readonly string[]): Promise<void>;
}

export interface WorkflowInput {
  through: Stage;
  noSemantic: boolean;
  allAssets: boolean;
  allViewableAssets: boolean;
  allAudioAssets: boolean;
}

const WorkflowState = Annotation.Root({
  through: Annotation<Stage>(),
  noSemantic: Annotation<boolean>(),
  allAssets: Annotation<boolean>(),
  allViewableAssets: Annotation<boolean>(),
  allAudioAssets: Annotation<boolean>(),
  hasReferences: Annotation<boolean>(),
  completed: Annotation<Stage[]>({
    reducer: (left, right) => left.concat(right),
    default: () => [],
  }),
});

type State = typeof WorkflowState.State;

const semanticArgs = (state: State): string[] => (state.noSemantic ? ["--no-semantic"] : []);

const planArgs = (state: State): string[] => {
  const args = semanticArgs(state);
  if (state.allAssets) args.push("--all-assets");
  if (state.allViewableAssets) args.push("--all-viewable-assets");
  if (state.allAudioAssets) args.push("--all-audio-assets");
  return args;
};

function continuesAfter(state: State, stage: Stage, next: Stage): typeof END | Stage {
  return state.through === stage ? END : next;
}

/**
 * Creates the LangGraph state machine. Each stage node invokes one Go command;
 * LangGraph owns every edge and the only conditional route in this graph.
 */
export function createWorkflow(tools: GoTools) {
  const runStage = (stage: Stage, args: (state: State) => string[] = () => []) => async (state: State) => {
    await tools.runStage(stage, args(state));
    return { completed: [stage] };
  };

  return new StateGraph(WorkflowState)
    .addNode("workflow-context", async () => {
      const context = await tools.readWorkflowContext();
      return { hasReferences: context.hasReferences };
    })
    .addNode("ingest", runStage("ingest"))
    .addNode("align", runStage("align"))
    .addNode("analyze-assets", runStage("analyze-assets", semanticArgs))
    .addNode("analyze-style", runStage("analyze-style", semanticArgs))
    .addNode("plan", runStage("plan", planArgs))
    .addNode("render", runStage("render", () => ["--force"]))
    .addEdge(START, "workflow-context")
    .addEdge("workflow-context", "ingest")
    .addConditionalEdges("ingest", (state) => continuesAfter(state, "ingest", "align"))
    .addConditionalEdges("align", (state) => continuesAfter(state, "align", "analyze-assets"))
    .addConditionalEdges("analyze-assets", (state) => {
      if (state.through === "analyze-assets") return END;
      return state.hasReferences ? "analyze-style" : "plan";
    })
    .addConditionalEdges("analyze-style", (state) => continuesAfter(state, "analyze-style", "plan"))
    .addConditionalEdges("plan", (state) => continuesAfter(state, "plan", "render"))
    .addEdge("render", END)
    .compile();
}

export async function runWorkflow(tools: GoTools, input: WorkflowInput): Promise<readonly Stage[]> {
  const result = await createWorkflow(tools).invoke({
    ...input,
    hasReferences: false,
    completed: [],
  });
  return result.completed;
}
