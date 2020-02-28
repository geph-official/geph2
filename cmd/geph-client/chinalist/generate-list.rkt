#lang racket
(require net/url)

(define seen (mutable-set))

(define (generate-list (name "geolocation-cn"))
  (define url (string->url
               (format "https://raw.githubusercontent.com/v2ray/domain-list-community/master/data/~a" name)))
  (for ([line (in-lines (get-pure-port url))])
    (match (string-trim line)
      [(or "" (regexp #rx"^#")) (void)]
      [(regexp #rx"^include:")
       (generate-list (car (string-split (substring line 8))))]
      [(regexp #rx":") (void)]
      [x
       (define element (car (string-split x)))
       (unless (set-member? seen element)
         (set-add! seen element)
         (printf "~v : true,\n" (car (string-split x))))])))

(displayln "package chinalist")
(displayln "var domainList = map[string]bool{")
(generate-list)
(displayln "}")