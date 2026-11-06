import { GoCLITools, type GoCommand } from "./go-tools.js";
import { runWorkflow, stages, type Stage, type WorkflowInput } from "./graph.js";

function usage(): string {
  return `Usage: npm run workflow -- [options]

Options:
  --project <directory>          project directory (default: current directory)
  --through <stage>              ingest, align, analyze-assets, analyze-style, plan, or render (default: plan)
  --no-semantic                  disable semantic analysis and planning
  --all-assets                   require all eligible assets in the plan
  --all-viewable-assets          require all images and videos in the plan
  --all-audio-assets             include all sound-effect assets in the plan
  --go-command <path>            use a built auto-editor executable instead of go run
  --help                         show this message`;
}

function parseArgs(values: readonly string[]): { project: string; command?: GoCommand; input: WorkflowInput } {
  const input: WorkflowInput = {
    through: "plan",
    noSemantic: false,
    allAssets: false,
    allViewableAssets: false,
    allAudioAssets: false,
  };
  let project = process.cwd();
  let command: GoCommand | undefined;

  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    switch (value) {
      case "--help":
      case "-h":
        console.log(usage());
        process.exit(0);
      case "--project":
        project = requiredValue(values, ++index, value);
        break;
      case "--through": {
        const stage = requiredValue(values, ++index, value);
        if (!isStage(stage)) throw new Error(`invalid --through value ${JSON.stringify(stage)}; expected ${stages.join(", ")}`);
        input.through = stage;
        break;
      }
      case "--no-semantic":
        input.noSemantic = true;
        break;
      case "--all-assets":
        input.allAssets = true;
        break;
      case "--all-viewable-assets":
        input.allViewableAssets = true;
        break;
      case "--all-audio-assets":
        input.allAudioAssets = true;
        break;
      case "--go-command":
        command = { command: requiredValue(values, ++index, value), args: [] };
        break;
      default:
        throw new Error(`unknown option ${JSON.stringify(value)}`);
    }
  }
  return { project, command, input };
}

function requiredValue(values: readonly string[], index: number, option: string): string {
  const value = values[index];
  if (!value || value.startsWith("--")) throw new Error(`${option} requires a value`);
  return value;
}

function isStage(value: string): value is Stage {
  return (stages as readonly string[]).includes(value);
}

try {
  const options = parseArgs(process.argv.slice(2));
  const completed = await runWorkflow(new GoCLITools(options), options.input);
  console.log(`ok: LangGraph workflow completed through ${options.input.through} (${completed.join(", ")})`);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
