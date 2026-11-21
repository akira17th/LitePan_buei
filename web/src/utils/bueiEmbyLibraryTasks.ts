import type { StrmTask } from "@/api/strm";

const ARCHIVE_WORD = "归档";

function normalized(value: string) {
  return value.trim().toLocaleLowerCase();
}

function trimArchiveSeparators(value: string) {
  return value.replace(/^[\s·•|/\\_\-:：]+|[\s·•|/\\_\-:：]+$/g, "");
}

// 媒体库与 STRM 任务的匹配规则：
// - 已禁用（status=paused）的任务不参与匹配；
// - 普通媒体库不读取归档任务；
// - 归档媒体库只匹配名称同样含“归档”的任务，并按去掉“归档”后的名称区分电影/剧集；
// - 不会回退到普通（不含“归档”）任务，避免把普通任务的项目误认为归档内容。
export function matchBueiEmbyLibraryStrmTasks(tasks: StrmTask[], libraryName: string) {
  const name = normalized(libraryName);
  if (!name) return [] as StrmTask[];
  const enabled = tasks.filter((task) => task.status !== "paused");

  if (!name.includes(ARCHIVE_WORD)) {
    return enabled.filter((task) => {
      const taskName = normalized(task.name);
      return !taskName.includes(ARCHIVE_WORD) && taskName.includes(name);
    });
  }

  const baseName = trimArchiveSeparators(normalized(name.replaceAll(ARCHIVE_WORD, "")));
  if (!baseName) return [] as StrmTask[];
  return enabled.filter((task) => {
    const taskName = normalized(task.name);
    return taskName.includes(ARCHIVE_WORD) && taskName.includes(baseName);
  });
}
