import axios from 'axios'
import { defineStore } from 'pinia'
import { ref } from 'vue'

type ContentFilterSettings = {
  video_only?: boolean
  show_restricted_tags?: boolean
  show_episode_tags?: boolean
}

type SettingsResponse = {
  ui_settings?: {
    content_filters?: ContentFilterSettings
  }
}

export const useContentFiltersStore = defineStore('contentFilters', () => {
  const videoOnly = ref(false)
  const showRestrictedTags = ref(false)
  const showEpisodeTags = ref(false)
  const loaded = ref(false)
  let loadingPromise: Promise<void> | null = null

  const applySavedSettings = (settings?: ContentFilterSettings) => {
    videoOnly.value = settings?.video_only === true
    showRestrictedTags.value = settings?.show_restricted_tags === true
    showEpisodeTags.value = settings?.show_episode_tags === true
    loaded.value = true
  }

  const load = async (force = false) => {
    if (!force && loaded.value) return
    if (!force && loadingPromise) return loadingPromise

    loadingPromise = axios
      .get<SettingsResponse>('/api/settings')
      .then((response) => {
        applySavedSettings(response.data.ui_settings?.content_filters)
      })
      .catch(() => {
        applySavedSettings()
      })
      .finally(() => {
        loaded.value = true
        loadingPromise = null
      })
    return loadingPromise
  }

  const appendQuery = (params: URLSearchParams) => {
    params.set('video_only', videoOnly.value ? '1' : '0')
  }

  const appendSeedTagQuery = (params: URLSearchParams, includeReviewed: boolean) => {
    params.set('hide_restricted_tags', showRestrictedTags.value ? '0' : '1')
    params.set('hide_episode_tags', showEpisodeTags.value ? '0' : '1')
    if (includeReviewed && (showRestrictedTags.value || showEpisodeTags.value)) {
      params.set('include_tagged_reviewed', '1')
    }
  }

  return {
    videoOnly,
    showRestrictedTags,
    showEpisodeTags,
    loaded,
    load,
    applySavedSettings,
    appendQuery,
    appendSeedTagQuery,
  }
})
