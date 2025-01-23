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

  namespace assert {
    function fail(message: string): void;
    function equal(expected: any, actual: any): void;
    function notEqual(expected: any, actual: any): void;
    function containsString(substr: string, str: string): void;
    function containsElement(element: any, array: any[]): void;
  }
}

export {};
