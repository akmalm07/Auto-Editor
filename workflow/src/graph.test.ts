import assert from "node:assert/strict";
import test from "node:test";

import { runWorkflow, type GoTools, type Stage } from "./graph.js";

function fakeTools(hasReferences: boolean, calls: Stage[]): GoTools {
  return {
    async readWorkflowContext() {
      return { hasReferences };
    },
    async runStage(stage) {
      calls.push(stage);
    },
  };
}

test("LangGraph skips style analysis when the project has no references", async () => {
  const calls: Stage[] = [];
  const completed = await runWorkflow(fakeTools(false, calls), {
    through: "plan",
    noSemantic: false,
    allAssets: false,
    allViewableAssets: false,
    allAudioAssets: false,
  });
  assert.deepEqual(calls, ["ingest", "align", "analyze-assets", "plan"]);
  assert.deepEqual(completed, calls);
});

test("LangGraph routes through style analysis when reference sets exist", async () => {
  const calls: Stage[] = [];
  await runWorkflow(fakeTools(true, calls), {
    through: "render",
    noSemantic: true,
    allAssets: false,
    allViewableAssets: true,
    allAudioAssets: true,
  });
  assert.deepEqual(calls, ["ingest", "align", "analyze-assets", "analyze-style", "plan", "render"]);
});

test("LangGraph sends only the applicable Go flags to each node", async () => {
  const invocations: Array<{ stage: Stage; args: readonly string[] }> = [];
  const tools: GoTools = {
    async readWorkflowContext() {
      return { hasReferences: false };
    },
    async runStage(stage, args) {
      invocations.push({ stage, args });
    },
  };
  await runWorkflow(tools, {
    through: "plan",
    noSemantic: true,
    allAssets: true,
    allViewableAssets: true,
    allAudioAssets: true,
  });
  assert.deepEqual(invocations, [
    { stage: "ingest", args: [] },
    { stage: "align", args: [] },
    { stage: "analyze-assets", args: ["--no-semantic"] },
    { stage: "plan", args: ["--no-semantic", "--all-assets", "--all-viewable-assets", "--all-audio-assets"] },
  ]);
});
