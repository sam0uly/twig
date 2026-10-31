;; A bounded cache of recent keys.
(ns fun.twig.cache
  (:require [clojure.string :as str]))

(def ^:private default-capacity 16)

(defn create
  ([] (create default-capacity))
  ([capacity] (atom {:capacity capacity :slots []})))

(defn push [cache key]
  (swap! cache
         (fn [{:keys [capacity slots]}]
           {:capacity capacity
            :slots (take capacity (cons key slots))})))

(defn cached? [cache key]
  (boolean (some #{key} (:slots @cache))))

(defn report [cache]
  (let [{:keys [slots]} @cache]
    (str "{\"kind\": \"cache\", \"size\": " (count slots) "}")))

;; Numbers in several bases.
(def mask (bit-and 0xff 0b1111))
(def ratio 1.5e3)

(comment
  (push (create 4) "one")
  (str/upper-case "keys"))
