import { computed, ref, watch, type Ref } from "vue";
import type { RouteLocationNormalizedLoaded, Router } from "vue-router";
import type { Crumb } from "@/stores/browser";
import {
  browserLocationKey,
  buildBrowserLocationQuery,
  parseBrowserLocationQuery,
  stripBrowserLocationQuery,
  type BrowserUrlLocation,
} from "@/utils/bueiBrowserLocation";

export interface UseBueiBrowserUrlSyncOptions {
  route: RouteLocationNormalizedLoaded;
  router: Router;
  accountId: Ref<number | null>;
  breadcrumb: Ref<Crumb[]>;
  loading: Ref<boolean>;
  bootstrapping: Ref<boolean>;
  accountExists: (accountId: number) => boolean;
  applyLocation: (location: BrowserUrlLocation, opts?: { silent?: boolean }) => Promise<void>;
  onInvalid?: (reason: string) => void;
}

/**
 * 把文件浏览器的账号 + 面包屑同步到 URL query，并在浏览器前进/后退时恢复。
 *
 * URL 约定：
 *   /?account=1
 *   /?account=1&dir=v1.<base64url>
 *
 * dir 只编码非根面包屑；根目录不需要 dir。
 */
export function useBueiBrowserUrlSync(options: UseBueiBrowserUrlSyncOptions) {
  const { route, router, accountId, breadcrumb, loading, bootstrapping } = options;
  const started = ref(false);
  const applyingFromRoute = ref(false);
  let routeChangeSeq = 0;
  let lastUrlKey = "";

  function currentLocation(): BrowserUrlLocation | null {
    if (accountId.value == null) return null;
    const crumbs = breadcrumb.value.length
      ? breadcrumb.value.map((crumb) => ({ id: crumb.id, name: crumb.name }))
      : [{ id: "", name: "根目录" }];
    return { accountId: accountId.value, crumbs };
  }

  function currentKey(): string {
    return browserLocationKey(currentLocation());
  }

  async function replaceWithCurrent() {
    // 组件卸载/切到 admin、login 的过渡期不要再把 account/dir 写到别的路由上。
    if (route.name !== "home") return;
    const location = currentLocation();
    if (!location) {
      lastUrlKey = "";
      try {
        await router.replace({
          path: route.path,
          query: stripBrowserLocationQuery(route.query),
        });
      } catch {
        // 无账号时只做 URL 清理，失败不影响首屏。
      }
      return;
    }

    const key = browserLocationKey(location);
    lastUrlKey = key;
    try {
      await router.replace({
        path: route.path,
        query: buildBrowserLocationQuery(location, route.query),
      });
    } catch {
      if (lastUrlKey === key) lastUrlKey = "";
    }
  }

  async function handleRouteQuery() {
    if (!started.value || bootstrapping.value || route.name !== "home") return;

    const seq = ++routeChangeSeq;
    const parsed = parseBrowserLocationQuery(route.query);

    if (parsed.status === "none") {
      applyingFromRoute.value = false;
      await replaceWithCurrent();
      return;
    }
    if (parsed.status === "invalid") {
      applyingFromRoute.value = false;
      options.onInvalid?.(parsed.reason);
      await replaceWithCurrent();
      return;
    }

    const target = parsed.location;
    const targetKey = browserLocationKey(target);
    if (targetKey === currentKey()) {
      applyingFromRoute.value = false;
      lastUrlKey = targetKey;
      return;
    }
    if (!options.accountExists(target.accountId)) {
      applyingFromRoute.value = false;
      options.onInvalid?.("账号不存在或不可见");
      await replaceWithCurrent();
      return;
    }

    applyingFromRoute.value = true;
    try {
      await options.applyLocation(target, { silent: false });
    } catch {
      // applyLocation 内部通常已处理错误；这里兜底，后面按实际位置回写 URL。
    } finally {
      if (seq === routeChangeSeq) {
        applyingFromRoute.value = false;
      }
    }
    if (seq !== routeChangeSeq) return;

    const afterKey = currentKey();
    lastUrlKey = afterKey;
    if (afterKey !== targetKey) {
      await replaceWithCurrent();
    }
  }

  watch(
    () => [route.query.account, route.query.dir] as const,
    () => {
      void handleRouteQuery();
    },
    { flush: "post" },
  );

  const settledLocationKey = computed(() => {
    if (!started.value || bootstrapping.value || loading.value || applyingFromRoute.value) {
      return "";
    }
    return currentKey();
  });

  watch(
    settledLocationKey,
    (key) => {
      if (!key || key === lastUrlKey || route.name !== "home") return;
      const location = currentLocation();
      if (!location) return;
      lastUrlKey = key;
      void router
        .push({
          path: route.path,
          query: buildBrowserLocationQuery(location, route.query),
        })
        .catch(() => {
          // 导航被守卫取消时不保留“已同步”的假状态，下一次变化会重试。
          if (lastUrlKey === key) lastUrlKey = "";
        });
    },
    { flush: "post" },
  );

  /** 初次定位完成后调用；会顺便把当前地址规范化。 */
  async function start() {
    started.value = true;
    await replaceWithCurrent();
  }

  return {
    start,
    replaceWithCurrent,
    currentLocation,
    currentKey,
  };
}
