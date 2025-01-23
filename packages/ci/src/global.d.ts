declare global {
  interface Task {
    name: string;
    image: string;
    command: string[];
  }

  interface TaskResult {
    stdout: string;
    stderr: string;
    error: string;
    code: number;
  }

  function run(task: Task): TaskResult;
}

export {};
